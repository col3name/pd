package control

import (
	"fmt"
	"sync"
	"time"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/pipeline"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
	"github.com/kind-earthquake/pii-module/internal/resolve"
	"github.com/kind-earthquake/pii-module/internal/store"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

// Manager holds the live runtime state: config, detector, context, whitelist
// and all pipelines (base + per-system). Apply() rebuilds everything under a
// write lock and bumps the revision.
type Manager struct {
	mu           sync.RWMutex
	cfgPath      string
	cfg          *config.Config
	store        store.Store
	detectorOpts []detector.Option
	detector     *detector.Detector
	context      *context.Resolver
	whitelist    *whitelist.Whitelist
	base         *pipeline.Pipeline
	systems      map[string]*pipeline.Pipeline
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

// WithDetectorOpts appends extra detector options (e.g. the NER smart path).
// They are re-applied on every Apply(); NER model instance is kept in main.go
// and survives runtime config reloads.
func WithDetectorOpts(opts ...detector.Option) Option {
	return func(m *Manager) { m.detectorOpts = append(m.detectorOpts, opts...) }
}

// New builds a Manager. It requires either WithConfigPath or WithConfig.
func New(opts ...Option) (*Manager, error) {
	m := &Manager{systems: make(map[string]*pipeline.Pipeline), cfgPath: defaultConfigPath()}
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
	ctxR := context.New(nil, nil)
	if m.cfg.Context.Enabled {
		ctxR = context.New(m.cfg.Context.Boost, m.cfg.Context.Penalty)
	}
	var w *whitelist.Whitelist
	if m.cfg.Whitelist.Enabled {
		w = whitelist.New(m.cfg.Whitelist.Persons, m.cfg.Whitelist.Addresses, m.cfg.Whitelist.Organizations)
	}
	core := detector.StructuredRules()
	extra, err := m.buildOverlayRules()
	if err != nil {
		return err
	}
	allRules := append(core, extra...)
	d := detector.New(allRules, m.detectorOpts...)
	priority := m.cfg.Resolve.Priority
	if priority == nil {
		priority = copyPriority(resolve.DefaultPriority)
	}
	combos := make([]pipeline.Combination, 0, len(m.cfg.Combinations))
	for _, c := range m.cfg.Combinations {
		combos = append(combos, pipeline.Combination{Type: c.Type, Requires: c.Requires, Window: c.Window})
	}
	opts := pipeline.Options{
		Mode:         m.cfg.Masking.Mode,
		Sensitive:    m.cfg.SensitiveTypes,
		Combinations: combos,
	}
	m.base = pipeline.New(d, ctxR, w, priority, opts)
	m.systems = make(map[string]*pipeline.Pipeline, len(m.cfg.Systems))
	for i := range m.cfg.Systems {
		s := m.cfg.Systems[i]
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
	for i := range m.cfg.Systems {
		if m.cfg.Systems[i].Name == name {
			return &m.cfg.Systems[i]
		}
	}
	return nil
}

func (m *Manager) Rev() uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.rev
}

func (m *Manager) KnownTypes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	types := make([]string, 0, len(detector.KnownTypes())+len(m.cfg.Rules))
	for _, t := range detector.KnownTypes() {
		types = append(types, string(t))
	}
	for _, r := range m.cfg.Rules {
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

// BuildOverlay compiles cfg.Rules into detector rules.
func (m *Manager) BuildOverlay() ([]detector.Rule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.buildOverlayRules()
}

func (m *Manager) buildOverlayRules() ([]detector.Rule, error) {
	if len(m.cfg.Rules) == 0 {
		return nil, nil
	}
	rules := make([]detector.Rule, 0, len(m.cfg.Rules))
	for _, rc := range m.cfg.Rules {
		r, err := detector.RuleFromConfig(rc.Type, rc.Regex, rc.Context, rc.Capture, rc.Priority, rc.Confidence, rc.Keyword)
		if err != nil {
			return nil, err
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// Apply swaps in a new config, rebuilding all runtime components under the
// write lock. On error the previous state is restored untouched and rev does
// not change.
func (m *Manager) Apply(cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	oldCfg, oldDet, oldCtx, oldW, oldBase, oldSys, oldLim := m.cfg, m.detector, m.context, m.whitelist, m.base, m.systems, m.limiter
	m.cfg = cfg
	if err := m.buildLocked(); err != nil {
		m.cfg, m.detector, m.context, m.whitelist, m.base, m.systems, m.limiter = oldCfg, oldDet, oldCtx, oldW, oldBase, oldSys, oldLim
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
