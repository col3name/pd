package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/redis/go-redis/v9"

	openapi "github.com/kind-earthquake/pii-module"
	"github.com/kind-earthquake/pii-module/internal/api/handlers"
	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/ner"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to the YAML config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	ttl := time.Duration(cfg.Store.TTLHours) * time.Hour
	var st store.Store
	var redisClient *redis.Client
	if cfg.Store.Type == "redis" {
		redisClient = redis.NewClient(redisOptions(cfg.Store.RedisURL))
		st = store.NewRedis(redisClient, ttl)
	} else {
		st = store.NewMemory(ttl, cfg.Store.Capacity)
	}

	detectorOpts := []detector.Option{}
	var nerModel *ner.Model
	if cfg.ML.Enabled && cfg.ML.ModelPath != "" && cfg.ML.VocabPath != "" {
		labels := cfg.ML.Labels
		if len(labels) == 0 {
			labels = []string{"O", "B-PER", "I-PER", "B-LOC", "I-LOC", "B-ORG", "I-ORG"}
		}
		m, err := ner.NewModel(cfg.ML.ModelPath, cfg.ML.VocabPath, labels, 128)
		if err != nil {
			slog.Warn("failed to load NER model; continuing without NER", "error", err)
		} else {
			nerModel = m
			detectorOpts = append(detectorOpts, detector.WithNER(m))
			slog.Info("NER model loaded", "model", cfg.ML.ModelPath)
		}
	}

	mgr, err := control.New(
		control.WithConfig(cfg),
		control.WithStore(st),
		control.WithDetectorOpts(detectorOpts...),
	)
	if err != nil {
		slog.Error("failed to build pipeline", "error", err)
		os.Exit(1)
	}
	h := &handlers.Handler{Mgr: mgr}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Handle("/metrics", observability.Handler())
	r.Route("/v1", func(r chi.Router) {
		r.Use(handlers.CORS(adminOrigin()))
		r.Get("/config", h.GetConfig)
		r.Put("/config", h.PutConfig)
		r.Get("/config/rules", h.GetConfigRules)
	})
	r.With(handlers.CORS(adminOrigin())).Post("/process", h.Process)
	// OpenAPI: сама спецификация + интерактивный Swagger UI (Try it out).
	r.Get("/openapi.yaml", openapi.SpecHandler())
	r.Get("/process_api.yaml", openapi.SpecHandler())
	r.Get("/docs", openapi.DocsHandler())
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/docs", http.StatusFound)
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("pii-module listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	if redisClient != nil {
		_ = redisClient.Close()
	}
	if nerModel != nil {
		_ = nerModel.Close()
	}
}

// adminOrigin returns the CORS allow-origin for the admin/config endpoints
// (ADMIN_ORIGIN env, default "*").
func adminOrigin() string {
	origin := os.Getenv("ADMIN_ORIGIN")
	if origin == "" {
		return "*"
	}
	return origin
}

// redisOptions converts a redis URL (redis://host:port/db) into redis.Options,
// preserving the DB path (e.g. /1 selects logical database 1).
func redisOptions(redisURL string) *redis.Options {
	opts := &redis.Options{Addr: redisURL}
	if u, err := url.Parse(redisURL); err == nil && u.Host != "" {
		opts.Addr = u.Host
		if db := strings.TrimPrefix(u.Path, "/"); db != "" {
			if n, err := strconv.Atoi(db); err == nil {
				opts.DB = n
			}
		}
	}
	return opts
}
