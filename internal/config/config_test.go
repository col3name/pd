package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	require.Equal(t, "redact", cfg.Masking.Mode)
	require.Equal(t, "memory", cfg.Store.Type)
	require.True(t, cfg.AllowUnmask)
	require.False(t, cfg.ML.Enabled)
	require.Greater(t, cfg.Resolve.Priority[detector.TypeCard], cfg.Resolve.Priority[detector.TypeINN])
	require.Contains(t, cfg.SensitiveTypes, detector.TypePIN)
}

func TestLoadFromYAML(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "configs", "config.yaml"))
	require.NoError(t, err)
	require.Equal(t, "redact", cfg.Masking.Mode)
	require.Equal(t, "memory", cfg.Store.Type)
	require.True(t, cfg.Context.Enabled)
	require.Len(t, cfg.Whitelist.Persons, 3)
	require.Equal(t, 100, cfg.Resolve.Priority[detector.TypeCard])
	require.Equal(t, 40, cfg.Resolve.Priority[detector.TypeINN])
}
