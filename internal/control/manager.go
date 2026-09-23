package control

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kind-earthquake/pii-module/internal/config"
	ctxpkg "github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/pipeline"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
	"github.com/kind-earthquake/pii-module/internal/resolve"
	"github.com/kind-earthquake/pii-module/internal/store"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

// SystemSource is the subset of db.Repo the Manager needs to build pipelines.
type SystemSource interface {
	ListSystems(ctx context.Context) ([]config.SystemConfig, error)
	ListRules(ctx context.Context) ([]config.RuleConfig, error)
	ListCombinations(ctx context.Context) ([]config.CombinationConfig, error)
}

// Manager holds the live runtime state: config, detector, context, whitelist
// and all pipelines (base + per-system). Reload() rebuilds everything under a
// write lock and bumps the revision.
type Manager struct {
	mu           sync.RWMutex
	cfgPath      string
	cfg          *config.Config
	repo         SystemSource
	store        store.Store
	detectorOpts []detector.Option
	detector     *detector.Detector
	context      *ctxpkg.Resolver
	whitelist    *whitelist.Whitelist
	base         *pipeline.Pipeline
	systems      map[string]*pipeline.Pipeline
	systemCfg    map[string]config.SystemConfig // name -> config for auth
	rules        []config.RuleConfig
	limiter      *ratelimit.Limiter
	rev          uint64
}

// Option configures a Manager.
type Option func(*Manager)

// WithConfigPath loads the config from a YAML file at startup.
func WithConfigPath(path string) Option {
	return func(m *Manager) { m.cfgPath = path }
}

// WithConfig uses an already-loaded config.
func WithConfig(cfg *config.Config) Option {
	return func(m *Manager) { m.cfg = cfg }
}

// WithStore sets the payload store.
func WithStore(s store.Store) Option {
	return func(m *Manager) { m.store = s }
}

// WithRepo sets the system/rules/combinations source (Postgres).
func WithRepo(repo SystemSource) Option {
	return func(m *Manager) { m.repo = repo }
}

// WithDetectorOpts appends extra detector options (e.g. the NER smart path).
// They are re-applied on every Apply(); NER model instance is kept in main.go
// and survives runtime config reloads.
func WithDetectorOpts(opts ...detector.Option) Option {
	return func(m *Manager) { m.detectorOpts = append(m.detectorOpts, opts...) }
}

// New builds a Manager. It requires either WithConfigPath or WithConfig.
func New(opts ...Option) (*Manager, error) {
	m := &Manager{systems: make(map[string]*pipeline.Pipeline), systemCfg: make(map[string]config.SystemConfig), cfgPath: defaultConfigPath()}
	for _, o := range opts {
		o(m)
	}
	if m.cfg == nil {
		cfg, err := config.Load(m.cfgPath)
		if err != nil {
			return nil, fmt.Errorf("load config: %w", err)
		}
		m.cfg = cfg
	}
	if m.store == nil {
		ttl := time.Duration(m.cfg.Store.TTLHours) * time.Hour
		m.store = store.NewMemory(ttl, m.cfg.Store.Capacity)
	}
	if err := m.buildLocked(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) buildLocked() error {
	ctxR := ctxpkg.New(nil, nil)
	if m.cfg.Context.Enabled {
		ctxR = ctxpkg.New(m.cfg.Context.Boost, m.cfg.Context.Penalty)
	}
	var w *whitelist.Whitelist
	if m.cfg.Whitelist.Enabled {
		w = whitelist.New(m.cfg.Whitelist.Persons, m.cfg.Whitelist.Addresses, m.cfg.Whitelist.Organizations)
	}

	var rules []config.RuleConfig
	var combos []config.CombinationConfig
	var systems []config.SystemConfig
	if m.repo != nil {
		var err error
		systems, err = m.repo.ListSystems(context.Background())
		if err != nil {
			return fmt.Errorf("list systems: %w", err)
		}
		rules, err = m.repo.ListRules(context.Background())
		if err != nil {
			return fmt.Errorf("list rules: %w", err)
		}
		combos, err = m.repo.ListCombinations(context.Background())
		if err != nil {
			return fmt.Errorf("list combinations: %w", err)
		}
	} else {
		systems = m.cfg.Systems
		rules = m.cfg.Rules
		combos = m.cfg.Combinations
	}

	core := detector.StructuredRules()
	extra, err := m.buildOverlayRules(rules)
	if err != nil {
		return err
	}
	allRules := append(core, extra...)
	d := detector.New(allRules, m.detectorOpts...)
	m.detector = d
	m.context = ctxR
	m.whitelist = w
	m.rules = rules
	priority := m.cfg.Resolve.Priority
	if priority == nil {
		priority = copyPriority(resolve.DefaultPriority)
	}
	pc := make([]pipeline.Combination, 0, len(combos))
	for _, c := range combos {
		pc = append(pc, pipeline.Combination{Type: c.Type, Requires: c.Requires, Window: c.Window})
	}
	opts := pipeline.Options{
		Mode:         m.cfg.Masking.Mode,
		Sensitive:    m.cfg.SensitiveTypes,
		Combinations: pc,
	}
	m.base = pipeline.New(d, ctxR, w, priority, opts)
	m.systems = make(map[string]*pipeline.Pipeline, len(systems))
	m.systemCfg = make(map[string]config.SystemConfig, len(systems))
	for i := range systems {
		s := systems[i]
		m.systemCfg[s.Name] = s
		if !s.Enabled {
			continue
		}
		mode := s.Masking
		if mode == "" {
			mode = opts.Mode
		}
		so := opts
		so.Mode = mode
		so.AllowedTypes = s.PII
		m.systems[s.Name] = pipeline.New(d, ctxR, w, priority, so)
	}
	if m.cfg.RateLimit.RPS > 0 {
		m.limiter = ratelimit.New(m.cfg.RateLimit.RPS, m.cfg.RateLimit.Burst)
	} else {
		m.limiter = nil
	}
	return nil
}

func (m *Manager) Config() *config.Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

func (m *Manager) Pipeline(system string) *pipeline.Pipeline {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if system == "" {
		return m.base
	}
	if p, ok := m.systems[system]; ok {
		return p
	}
	return m.base
}

func (m *Manager) System(name string) *config.SystemConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.systemCfg[name]; ok {
		return &s
	}
	return nil
}

// SystemHash returns the api_key_hash for a system ("" if absent).
func (m *Manager) SystemHash(name string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.systemCfg[name]; ok {
		return s.APIKey
	}
	return ""
}

func (m *Manager) Rev() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rev
}

func (m *Manager) KnownTypes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	types := make([]string, 0, len(detector.KnownTypes())+len(m.rules))
	for _, t := range detector.KnownTypes() {
		types = append(types, string(t))
	}
	for _, r := range m.rules {
		found := false
		for _, k := range types {
			if k == r.Type {
				found = true
				break
			}
		}
		if !found {
			types = append(types, r.Type)
		}
	}
	return types
}

func (m *Manager) Store() store.Store {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.store
}

func (m *Manager) Limiter() *ratelimit.Limiter {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.limiter
}

func (m *Manager) buildOverlayRules(rules []config.RuleConfig) ([]detector.Rule, error) {
	if len(rules) == 0 {
		return nil, nil
	}
	out := make([]detector.Rule, 0, len(rules))
	for _, rc := range rules {
		r, err := detector.RuleFromConfig(rc.Type, rc.Regex, rc.Context, rc.Capture, rc.Priority, rc.Confidence, rc.Keyword)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Reload re-reads systems/rules/combinations from the repo and rebuilds all
// runtime components under the write lock. On error the previous state is
// restored untouched and rev does not change.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldDet, oldCtx, oldW, oldBase, oldSys, oldSysCfg, oldLim, oldRules :=
		m.detector, m.context, m.whitelist, m.base, m.systems, m.systemCfg, m.limiter, m.rules
	if err := m.buildLocked(); err != nil {
		m.detector, m.context, m.whitelist, m.base, m.systems, m.systemCfg, m.limiter, m.rules =
			oldDet, oldCtx, oldW, oldBase, oldSys, oldSysCfg, oldLim, oldRules
		return err
	}
	m.rev++
	return nil
}

func defaultConfigPath() string { return "configs/config.yaml" }

func copyPriority(in map[detector.Type]int) map[detector.Type]int {
	out := make(map[detector.Type]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
