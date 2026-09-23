package handlers

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

// cleanTables truncates all tables so each test starts from a clean state.
func cleanTables(t *testing.T) {
	t.Helper()
	ctx := t.Context()
	dsn := tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable")
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	_, err = pool.Exec(ctx,
		`TRUNCATE systems, rules, combinations, admins, sessions RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
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
	cleanTables(t)
	return &Handler{Mgr: m, Repo: repo}
}