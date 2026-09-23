package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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
	"github.com/kind-earthquake/pii-module/internal/db"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/ner"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/queue"
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

	// Optional log file: write structured logs to a file (for admin download)
	// in addition to stderr. Empty log_file keeps stderr-only logging.
	// If the file can't be opened (e.g. read-only volume), fall back to stderr
	// instead of crashing — the server must stay up.
	var logFile *os.File
	if cfg.LogFile != "" {
		logFile, err = os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			slog.Warn("failed to open log file; continuing with stderr only", "path", cfg.LogFile, "error", err)
		} else {
			defer logFile.Close()
			multi := io.MultiWriter(os.Stderr, logFile)
			slog.SetDefault(slog.New(slog.NewJSONHandler(multi, nil)))
		}
	}

	ttl := time.Duration(cfg.Store.TTLHours) * time.Hour
	var st store.Store
	var redisClient *redis.Client
	if cfg.Store.Type == "redis" || cfg.Store.Type == "layered" {
		redisClient = redis.NewClient(redisOptions(cfg.Store.RedisURL))
		redisStore := store.NewRedis(redisClient, ttl)
		if cfg.Store.Type == "layered" {
			local := store.NewMemory(ttl, cfg.Store.Capacity)
			breaker := store.NewBreaker(cfg.Store.Circuit.Failures, time.Duration(cfg.Store.Circuit.CooldownSeconds)*time.Second)
			st = store.NewLayered(redisStore, local, breaker)
		} else {
			st = redisStore
		}
	} else {
		st = store.NewMemory(ttl, cfg.Store.Capacity)
	}

	ctx := context.Background()
	var repo *db.Repo
	if cfg.Database.DSN != "" {
		repo, err = db.New(ctx, cfg.Database.DSN)
		if err != nil {
			slog.Error("failed to connect database", "error", err)
			os.Exit(1)
		}
		defer repo.Close()
		if err := repo.SeedFromConfig(ctx, cfg); err != nil {
			slog.Error("failed to seed database", "error", err)
			os.Exit(1)
		}
		if err := repo.SeedAdmin(ctx, cfg.Admin.Login, cfg.Admin.Password); err != nil {
			slog.Error("failed to seed admin", "error", err)
			os.Exit(1)
		}
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
		control.WithRepo(repo),
	)
	if err != nil {
		slog.Error("failed to build pipeline", "error", err)
		os.Exit(1)
	}
	h := &handlers.Handler{Mgr: mgr, Repo: repo}
	pool := queue.New(cfg.Queue.Workers, cfg.Queue.FastCapacity, cfg.Queue.HeavyCapacity, func(system, payload string) queue.Result {
		res := mgr.Pipeline(system).Process(payload)
		return queue.Result{Masked: res.Masked, Types: res.Types, Tokens: res.Tokens}
	})
	h.Pool = pool
	defer pool.Close()

	var layeredStore *store.LayeredStore
	if ls, ok := st.(*store.LayeredStore); ok {
		layeredStore = ls
	}
	reporterCtx, stopReporter := context.WithCancel(context.Background())
	defer stopReporter()
	observability.StartReporter(reporterCtx, pool, layeredStore, time.Duration(cfg.Autoscale.PollSeconds)*time.Second)

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Handle("/metrics", observability.Handler())
	r.Route("/v1", func(r chi.Router) {
		r.Use(handlers.CORS(adminOrigin()))
		r.Post("/auth/login", h.Login)
		r.Post("/auth/logout", h.RequireAuth(h.Logout))
		r.Group(func(r chi.Router) {
			r.Use(requireAuth(h))
			r.Get("/config", h.GetConfig)
			r.Get("/logs", h.GetLogs)
			r.Get("/systems", h.ListSystems)
			r.Post("/systems", h.CreateSystem)
			r.Get("/systems/{name}", h.GetSystem)
			r.Put("/systems/{name}", h.UpdateSystem)
			r.Delete("/systems/{name}", h.DeleteSystem)
			r.Post("/systems/{name}/regenerate-key", h.RegenerateKey)
		})
	})
	r.Route("/process", func(r chi.Router) {
		r.Use(handlers.CORS(adminOrigin()))
		r.Post("/", h.Process)
		r.Options("/", func(http.ResponseWriter, *http.Request) {})
	})
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

// requireAuth adapts handlers.RequireAuth (a http.HandlerFunc wrapper) into a
// chi middleware so it can be applied to a route group via r.Use.
func requireAuth(h *handlers.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.RequireAuth(next.ServeHTTP)(w, r)
		})
	}
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
