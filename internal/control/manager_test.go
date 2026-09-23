package control

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	require.NoError(t, writeTestConfig(path))
	m, err := New(WithConfigPath(path), WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	require.NotNil(t, m.Pipeline(""))
	require.Equal(t, uint64(0), m.Rev())
}

func TestApplyRebuildsConfig(t *testing.T) {
	m := mustManager(t)
	cfg := m.Config()
	cfg.Rules = []config.RuleConfig{
		{Type: "СНИЛС", Regex: `\d{3}-\d{3}-\d{3}\s\d{2}`, Priority: 0, Confidence: 0.99},
	}
	require.NoError(t, m.Apply(cfg))
	require.Equal(t, uint64(1), m.Rev())
	// Overlay rule is live in the new detector.
	p := m.Pipeline("")
	res := p.Process("снилс 123-456-789 01")
	require.Contains(t, res.Types, "СНИЛС")
}

func TestApplyBadRegexRejectedAndStateKept(t *testing.T) {
	m := mustManager(t)
	cfg := m.Config()
	cfg.Rules = []config.RuleConfig{{Type: "X", Regex: "("}}
	err := m.Apply(cfg)
	require.Error(t, err)
	// The old (valid) config is still active.
	require.Equal(t, uint64(0), m.Rev())
}

func writeTestConfig(path string) error {
	return config.WriteMinimal(path)
}

func mustManager(t *testing.T) *Manager {
	t.Helper()
	cfg := config.Default()
	m, err := New(WithConfig(cfg), WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	return m
}
