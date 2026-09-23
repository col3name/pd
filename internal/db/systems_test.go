package db

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

// newTestRepo connects to a real Postgres. Skip if PII_TEST_DSN is unset.
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	dsn := tEnv("PII_TEST_DSN", "postgres://pii:pii@localhost:5432/pii?sslmode=disable")
	ctx := context.Background()
	r, err := New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func tEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func TestSystemsCRUD(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	_ = r.DeleteSystem(ctx, "test-sys")

	err := r.CreateSystem(ctx, config.SystemConfig{Name: "test-sys", Enabled: true, AllowUnmask: true, Masking: "token", PII: []detector.Type{detector.TypePhone}})
	require.NoError(t, err)

	s, err := r.GetSystem(ctx, "test-sys")
	require.NoError(t, err)
	require.Equal(t, "test-sys", s.Name)
	require.True(t, s.Enabled)
	require.True(t, s.AllowUnmask)
	require.Equal(t, "token", s.Masking)
	require.Equal(t, []detector.Type{detector.TypePhone}, s.PII)

	err = r.UpdateSystem(ctx, "test-sys", config.SystemConfig{Name: "test-sys", Enabled: false})
	require.NoError(t, err)
	s2, _ := r.GetSystem(ctx, "test-sys")
	require.False(t, s2.Enabled)

	err = r.DeleteSystem(ctx, "test-sys")
	require.NoError(t, err)
	_, err = r.GetSystem(ctx, "test-sys")
	require.Error(t, err)
}