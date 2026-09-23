package control

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/store"
)

type fakeRepo struct {
	systems []config.SystemConfig
	rules   []config.RuleConfig
	combos  []config.CombinationConfig
}

func (f *fakeRepo) ListSystems(ctx context.Context) ([]config.SystemConfig, error) { return f.systems, nil }
func (f *fakeRepo) ListRules(ctx context.Context) ([]config.RuleConfig, error)     { return f.rules, nil }
func (f *fakeRepo) ListCombinations(ctx context.Context) ([]config.CombinationConfig, error) {
	return f.combos, nil
}

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.yaml"
	require.NoError(t, writeTestConfig(path))
	m, err := New(WithConfigPath(path), WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	require.NotNil(t, m.Pipeline(""))
	require.Equal(t, uint64(0), m.Rev())
}

func TestReloadRebuildsFromRepo(t *testing.T) {
	repo := &fakeRepo{
		rules: []config.RuleConfig{
			{Type: "СНИЛС", Regex: `\d{3}-\d{3}-\d{3}\s\d{2}`, Priority: 0, Confidence: 0.99},
		},
	}
	m, err := New(WithConfig(config.Default()), WithStore(store.NewMemory(time.Hour, 1000)), WithRepo(repo))
	require.NoError(t, err)
	require.Equal(t, uint64(0), m.Rev())
	p := m.Pipeline("")
	res := p.Process("снилс 123-456-789 01")
	require.Contains(t, res.Types, "СНИЛС")
	// Reload picks up a change.
	repo.rules = nil
	require.NoError(t, m.Reload())
	require.Equal(t, uint64(1), m.Rev())
	p2 := m.Pipeline("")
	res2 := p2.Process("снилс 123-456-789 01")
	require.NotContains(t, res2.Types, "СНИЛС")
}

func TestReloadBadRegexRejectedAndStateKept(t *testing.T) {
	repo := &fakeRepo{}
	m, err := New(WithConfig(config.Default()), WithStore(store.NewMemory(time.Hour, 1000)), WithRepo(repo))
	require.NoError(t, err)
	require.Equal(t, uint64(0), m.Rev())
	repo.rules = []config.RuleConfig{{Type: "X", Regex: "("}}
	err = m.Reload()
	require.Error(t, err)
	require.Equal(t, uint64(0), m.Rev())
}

func TestManagerSystemFromRepo(t *testing.T) {
	repo := &fakeRepo{
		systems: []config.SystemConfig{{Name: "chat", Enabled: true, AllowUnmask: true, Masking: "token"}},
	}
	m, err := New(WithConfig(config.Default()), WithStore(store.NewMemory(time.Hour, 1000)), WithRepo(repo))
	require.NoError(t, err)
	s := m.System("chat")
	require.NotNil(t, s)
	require.True(t, s.Enabled)
	require.Equal(t, "token", s.Masking)
	require.Equal(t, "", m.SystemHash("chat")) // no api_key_hash set
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