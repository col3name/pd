package main

import (
	"context"
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

	"github.com/kind-earthquake/pii-module/internal/api/handlers"
	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	redisClient := redis.NewClient(redisOptions(cfg.RedisURL))
	st := store.New(redisClient, cfg.StoreTTL)

	h := &handlers.Handler{
		Detector: detector.New(detector.StructuredRules()),
		Store:    st,
		Cfg:      cfg,
	}
	// Rate limiter: enabled only when RATE_LIMIT_RPS > 0.
	if cfg.RateLimitRPS > 0 {
		h.Limiter = ratelimit.New(cfg.RateLimitRPS, cfg.RateLimitBurst)
		slog.Info("rate limiting enabled", "rps", cfg.RateLimitRPS, "burst", cfg.RateLimitBurst)
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Handle("/metrics", observability.Handler())
	r.Post("/process", h.Process)

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