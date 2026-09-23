package handlers

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/db"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func tEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// newDBTestHandler builds a Handler with a real Postgres repo. Skips if the
// DB is unreachable (PII_TEST_DSN, default localhost).
func newDBTestHandler(t *testing.T, mutate ...func(*config.Config)) *Handler {
	t.Helper()
	cfg := config.Default()
	for _, fn := range mutate {
		fn(cfg)
	}
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	repo, err := db.New(t.Context(), tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable"))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	return &Handler{Mgr: m, Repo: repo}
}