package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("REDIS_URL", "")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 8080, cfg.Port)
	require.Equal(t, "redis://localhost:6379/0", cfg.RedisURL)
	require.True(t, cfg.AllowUnmask)
	require.NotEmpty(t, cfg.MaskTypes)
}

func TestLoadEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("REDIS_URL", "redis://r:6379/1")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 9090, cfg.Port)
	require.Equal(t, "redis://r:6379/1", cfg.RedisURL)
}