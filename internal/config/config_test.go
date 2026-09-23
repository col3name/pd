package config

import (
	"os"
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

func TestLoadAdminRulesCombinations(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	data := []byte(`admin:
  key: "sekret"
rules:
  - type: "СНИЛС"
    regex: "\\d{11}"
    priority: 0
    confidence: 0.9
combinations:
  - type: "ПИН"
    requires: ["КАРТА"]
    window: 80
`)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, "sekret", cfg.Admin.Key)
	require.Len(t, cfg.Rules, 1)
	require.Equal(t, "СНИЛС", cfg.Rules[0].Type)
	require.Equal(t, "\\d{11}", cfg.Rules[0].Regex)
	require.Equal(t, float32(0.9), cfg.Rules[0].Confidence)
	require.Len(t, cfg.Combinations, 1)
	require.Equal(t, detector.TypePIN, cfg.Combinations[0].Type)
	require.Equal(t, []detector.Type{detector.TypeCard}, cfg.Combinations[0].Requires)
	require.Equal(t, 80, cfg.Combinations[0].Window)
}
