# Admin UI + Runtime Config Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add live runtime config management (hot-reload via JSON API) and a React admin UI (TelegramUI) to the PII Gateway, scoring jury criteria 3.4/3.6/3.7.

**Architecture:** A new `internal/control` `Manager` holds the current `config.Config`, a rebuilt `Detector` (core rules + overlay rules from config), `Context`, `Whitelist`, base pipeline and per-system pipelines, swapping everything atomically on `PUT /v1/config` under an RWMutex. The HTTP handler now depends on `*control.Manager` instead of raw config/pipeline/limiter. A separate React SPA in `web/` (Vite dev + nginx prod) talks to `GET/PUT /v1/config` through CORS. Config changes travel as JSON only; YAML is only read at startup.

**Tech Stack:** Go 1.25, chi, `gopkg.in/yaml.v3`; React 18 + TypeScript + Vite + `@tanstack/react-query` + `@telegram-apps/telegram-ui` (v2.1.13). Tests: testify/require. Go module path: `github.com/kind-earthquake/pii-module`.

## Global Constraints

- Go version floor: `go 1.25.0` (go.mod already). Do not bump dependencies.
- Config API is **JSON-only**. YAML is used only at process startup (`config.Load`).
- `config.Config` fields keep their existing `yaml` tags; JSON round-trip goes through a dedicated payload/view struct in `internal/control`, never by marshalling `*config.Config` directly.
- Overlay rules append to core rules (`detector.StructuredRules()`); they may not override built-in types (REJECT an overlay that repeats the type of an existing core rule — actually NO: a core default for a type is not special; rules with the same Type just coexist and are resolved by priority. Do not reject duplicates; let priority resolve).
- New PII types from overlay rules get `Placeholder()` = `[TYPE]` automatically (fallback in `types.go`), no code changes needed.
- `Placeholder()`/`Valid()` in `types.go` are updated so unknown types are valid and render `[TYPE]`. Update the existing `types_test.go` case that asserts `Type("UNKNOWN").Valid() == false`.
- API keys are never returned by `GET /v1/config`; only `api_key_set: true/false`. Round-trip: `api_key: null` = unchanged, `""` = cleared, non-empty string = replaced.
- `PUT /v1/config` requires header `X-Admin-Key` matching `config.Admin.Key`. No key configured → 403.
- CORS middleware on `/v1/*` and `/process`; `ADMIN_ORIGIN` env (default `*`).
- Sensitive-type metadata `pii_requests_total` etc. (observability) must keep working unchanged.
- Never change `internal/store` interface or `Entry` shape.
- Go build: `CGO_ENABLED=0 go build ./...` must pass (matches Dockerfile).
- Run `gofmt` before committing. Commit after every task.

---

### Task 1: Config — admin key, overlay rules, combinations, JSON payload types

**Files:**
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `config.AdminConfig{Key string}` (yaml `admin:`, key `key`).
  - `config.RuleConfig{Type string; Regex string; Priority int; Context string; Capture string; Keyword string; Confidence float32}` with yaml tags `type,regex,priority,context,capture,keyword,confidence`.
  - `config.CombinationConfig{Type detector.Type; Requires []detector.Type; Window int}` with yaml tags `type,requires,window`.
  - `config.Config` gains: `Admin AdminConfig`, `Rules []RuleConfig`, `Combinations []CombinationConfig`.
  - `config.Default()` initializes `Admin{Key: "pii-admin-key"}` and empty `Rules`/`Combinations`.

- [ ] **Step 1: Write the failing test**

Append to `internal/config/config_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadAdminRulesCombinations -v`
Expected: FAIL — unknown field `admin`/`rules`/`combinations`.

- [ ] **Step 3: Add the structs and fields**

In `internal/config/config.go`, add:

```go
// AdminConfig guards config writes (PUT /v1/config).
type AdminConfig struct {
	Key string `yaml:"key"`
}

// RuleConfig is a user-defined PII detection rule (overlay on top of core).
type RuleConfig struct {
	Type       string  `yaml:"type"`
	Regex      string  `yaml:"regex"`
	Priority   int     `yaml:"priority"`
	Context    string  `yaml:"context"` // regex matched in text before the span
	Capture    string  `yaml:"capture"` // regex with the span value in group 1
	Keyword    string  `yaml:"keyword"` // cheap substring pre-check (capture rules)
	Confidence float32 `yaml:"confidence"`
}

// CombinationConfig links a sensitive type to required co-occurring types.
type CombinationConfig struct {
	Type     detector.Type   `yaml:"type"`
	Requires []detector.Type `yaml:"requires"`
	Window   int             `yaml:"window"` // byte proximity
}
```

Add to `Config` struct:

```go
	Admin        AdminConfig            `yaml:"admin"`
	Rules        []RuleConfig           `yaml:"rules"`
	Combinations []CombinationConfig    `yaml:"combinations"`
```

In `Default()`:

```go
	return &Config{
		Port:            8080,
		AllowUnmask:     true,
		Store:           StoreConfig{Type: "memory", TTLHours: 24, Capacity: 1 << 18},
		Masking:         MaskingConfig{Mode: "redact"},
		Context:         ContextConfig{Enabled: true},
		Whitelist:       WhitelistConfig{Enabled: true},
		Resolve:         ResolveConfig{Priority: priority},
		SensitiveTypes:  []detector.Type{detector.TypePIN, detector.TypeCVV},
		Admin:           AdminConfig{Key: "pii-admin-key"},
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -v`
Expected: PASS (both old and new tests).

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "config: add admin key, overlay rules and combinations sections"
```

---

### Task 2: Detector — overlay rules, placeholder fallback for unknown types

**Files:**
- Modify: `internal/detector/types.go`, `internal/detector/rules.go`, `internal/detector/types_test.go`
- Test: `internal/detector/types_test.go`, `internal/detector/rules_test.go` (create)

**Interfaces:**
- Produces:
  - `func RuleFromConfig(typeName, regex, contextRe, captureRe string, priority int, confidence float32, keyword string) (Rule, error)` — compiles a user overlay rule; error on bad regexp. (Flat params: `detector` cannot import `config`.)
  - `detector.WithExtraRules(rules []Rule) Option` — appends to the built-in rule set.
  - `detector.KnownTypes() []Type` — returns all built-in type constants (for the UI checkbox list).
  - Updated `Placeholder()`: unknown types → `"[" + string(t) + "]"`.
  - Updated `Valid()`: unknown types → `true`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/detector/types_test.go`:

```go
func TestTypeUnknownPlaceholderFallback(t *testing.T) {
	got := Type("СНИЛС").Placeholder()
	require.Equal(t, "[СНИЛС]", got)
	require.True(t, Type("СНИЛС").Valid())
}
```

Create `internal/detector/rules_test.go` (NOTE: detector cannot import `config` — config imports detector. So `RuleFromConfig` takes flat params, no config import):

```go
package detector

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuleFromConfigValid(t *testing.T) {
	r, err := RuleFromConfig("СНИЛС", `\d{3}-\d{3}-\d{3}\s\d{2}`, "", "", 0, 0.99, "")
	require.NoError(t, err)
	require.Equal(t, Type("СНИЛС"), r.Type)
	require.Equal(t, float32(0.99), r.Confidence)

	d := New(StructuredRules(), WithExtraRules([]Rule{r}))
	spans := d.Detect("снилс 123-456-789 01")
	require.NotEmpty(t, spans)
}

func TestRuleFromConfigBadRegex(t *testing.T) {
	_, err := RuleFromConfig("X", "(", "", "", 0, 0, "")
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/detector/ -run 'TestTypeUnknownPlaceholderFallback|TestRuleFromConfig' -v`
Expected: FAIL — `RuleFromConfig`/`WithExtraRules` undefined; unknown type still invalid.

- [ ] **Step 3: Implement**

In `internal/detector/rules.go`, add at the bottom:

```go
// RuleFromConfig compiles a user-supplied rule (from the config rules section).
// Detector must not import config (config imports detector), so parameters are
// passed as plain values.
func RuleFromConfig(typeName, regex, contextRe, captureRe string, priority int, confidence float32, keyword string) (Rule, error) {
	r := Rule{
		Type:       Type(typeName),
		Priority:   priority,
		Keyword:    keyword,
		Confidence: confidence,
	}
	var err error
	if regex != "" {
		if r.Re, err = regexp.Compile(regex); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad regex: %w", typeName, err)
		}
	} else {
		return Rule{}, fmt.Errorf("rule %s: regex is required", typeName)
	}
	if contextRe != "" {
		if r.ContextRe, err = regexp.Compile(contextRe); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad context: %w", typeName, err)
		}
	}
	if captureRe != "" {
		if r.CaptureRe, err = regexp.Compile(captureRe); err != nil {
			return Rule{}, fmt.Errorf("rule %s: bad capture: %w", typeName, err)
		}
	}
	return r, nil
}
```

(Add `fmt` to the imports in `rules.go`. `regexp` is already imported.)

In `internal/detector/detector.go`, add:

```go
// WithExtraRules appends user-defined rules to the built-in core rules.
func WithExtraRules(rules []Rule) Option {
	return func(d *Detector) { d.rules = append(d.rules, rules...) }
}
```

In `internal/detector/rules.go` add (no config import needed):

```go
// KnownTypes returns every built-in PII type, sorted, for UI pickers.
func KnownTypes() []Type {
	types := make([]Type, 0, len(TypeList))
	types = append(types, TypeList...)
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}
```

Define `TypeList` in `types.go`:

```go
// TypeList is the full set of built-in PII types.
var TypeList = []Type{
	TypeAddress, TypeBirthCertificate, TypeBirthDate, TypeBirthPlace,
	TypeCard, TypeCardholder, TypeCitizenship, TypeCVV, TypeDate,
	TypeDeptCode, TypeDriverLicense, TypeEmail, TypeFIO, TypeForeignPassport,
	TypeINN, TypeIssuer, TypeMilitaryID, TypePassport, TypePassportIssue,
	TypePhone, TypePIN,
}
```

Update `types.go`:

```go
// Placeholder returns the masking token for the type.
func (t Type) Placeholder() string {
	if p, ok := placeholders[t]; ok {
		return p
	}
	return "[" + string(t) + "]"
}

// Valid reports whether t can be masked. Unknown types (from overlay rules)
// are valid: they render as [TYPE] via the placeholder fallback.
func (t Type) Valid() bool { return t != "" }
```

Update the existing test in `internal/detector/types_test.go`: replace `require.False(t, Type("UNKNOWN").Valid())` with nothing (now covered by the new test).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/detector/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/detector/
git commit -m "detector: overlay rules option, known types, placeholder fallback for custom types"
```

---

### Task 3: Pipeline — combination rules (PIN requires CARD)

**Files:**
- Modify: `internal/pipeline/pipeline.go`
- Test: `internal/pipeline/pipeline_test.go`

**Interfaces:**
- Produces:
  - `type Combination struct { Type detector.Type; Requires []detector.Type; Window int }` in `pipeline` package.
  - `pipeline.Options.Combinations []Combination`.
  - Behavior: a span whose type appears in `Combinations` is only kept if at least one `Requires` type appears within `Window` bytes of it; otherwise the span is dropped.

- [ ] **Step 1: Write the failing test**

Append to `internal/pipeline/pipeline_test.go`:

```go
func TestCombinationPINRequiresCard(t *testing.T) {
	d := detector.New(detector.StructuredRules())
	p := New(
		d,
		context.New(context.DefaultBoost, context.DefaultPenalty),
		whitelist.New(nil, nil, nil),
		resolve.DefaultPriority,
		Options{
			Combinations: []Combination{
				{Type: detector.TypePIN, Requires: []detector.Type{detector.TypeCard}, Window: 80},
			},
		},
	)
	res := p.Process("пин 1234")
	require.NotContains(t, res.Types, detector.TypePIN)

	res2 := p.Process("карта 4276 1234 5678 9012, пин 1234")
	require.Contains(t, res2.Types, detector.TypePIN)
	require.Contains(t, res2.Types, detector.TypeCard)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pipeline/ -run TestCombinationPINRequiresCard -v`
Expected: FAIL — `Combination` undefined; lone PIN gets masked today.

- [ ] **Step 3: Implement**

In `internal/pipeline/pipeline.go`:

```go
// Combination masks a type only when another PII type appears nearby.
type Combination struct {
	Type     detector.Type
	Requires []detector.Type
	Window   int // byte proximity; 0 = default 80
}
```

Add field to `Options`:

```go
	// Combinations mask a sensitive type only when a required co-occurring
	// type is present within Window bytes.
	Combinations []Combination
```

Add the filter right after `escalateDates` in `Process`:

```go
	spans := p.detector.Detect(text)
	spans = p.escalateDates(text, spans)
	spans = p.applyCombinations(spans)
	spans = resolve.Resolve(spans, p.priority)
```

Add methods:

```go
// applyCombinations drops spans whose combination requires another, absent type.
func (p *Pipeline) applyCombinations(in []detector.Span) []detector.Span {
	if len(p.opts.Combinations) == 0 {
		return in
	}
	kept := make([]detector.Span, 0, len(in))
	for _, s := range in {
		if p.passesCombination(s, in) {
			kept = append(kept, s)
		}
	}
	return kept
}

func (p *Pipeline) passesCombination(s detector.Span, spans []detector.Span) bool {
	for _, c := range p.opts.Combinations {
		if s.Type != c.Type {
			continue
		}
		window := c.Window
		if window == 0 {
			window = 80
		}
		found := false
		for _, o := range spans {
			if o.Type == s.Type {
				continue
			}
			if !typeInList(o.Type, c.Requires) {
				continue
			}
			if proximity(s, o) <= window {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func typeInList(t detector.Type, list []detector.Type) bool {
	for _, x := range list {
		if x == t {
			return true
		}
	}
	return false
}

func proximity(a, b detector.Span) int {
	switch {
	case b.End <= a.Start:
		return a.Start - b.End
	case a.End <= b.Start:
		return b.Start - a.End
	default:
		return 0 // overlap
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pipeline/ -run TestCombinationPINRequiresCard -v`
Expected: PASS.

Also run the full existing suite:
Run: `go test ./internal/pipeline/ ./internal/resolve/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pipeline/pipeline.go internal/pipeline/pipeline_test.go
git commit -m "pipeline: configurable combination masking (PIN requires CARD)"
```

---

### Task 4: control.Manager — runtime config holder and atomic rebuild

**Files:**
- Create: `internal/control/manager.go`, `internal/control/manager_test.go`
- Modify: `cmd/server/main.go` (moved logic), `internal/api/handlers/process.go` (dependent refactor comes in Task 5)

**Interfaces:**
- Produces:
  - `type Manager struct{ ... }` in `internal/control`.
  - `func New(options ...Option) (*Manager, error)` — builds a manager from `config.Load(cfgPath)`.
  - `Option func(*Manager)`, `WithConfigPath(path string)`, `WithStore(store.Store)`, `WithConfig(cfg *config.Config)`.
  - `(*Manager).Config() *config.Config`
  - `(*Manager).Pipeline(system string) *pipeline.Pipeline` — base or per-system (current `systemPipeline` logic).
  - `(*Manager).System(name string) *config.SystemConfig` — nil when unknown (current `systemConfig`).
  - `(*Manager).Apply(cfg *config.Config) error` — rebuild detector/context/whitelist/pipelines and swap under lock. Returns error on bad overlay rule regex.
  - `(*Manager).Rev() uint64` — incrementing revision on each Apply.
  - `(*Manager).KnownTypes() []string` — built-in + overlay types.
  - `(*Manager).Store() store.Store` — accessor for the handler.
  - `(*Manager).Limiter() *ratelimit.Limiter` — rebuilt from cfg.RateLimit on each Apply (nil when rps==0).
  - `(*Manager).BuildOverlay() ([]detector.Rule, error)` — compiles `cfg.Rules` via `detector.RuleFromConfig(type, regex, context, capture, priority, confidence, keyword)` (flat params; no import cycle). Called as `buildOverlayRules()` inside `buildLocked`.

- [ ] **Step 1: Write the failing test**

Create `internal/control/manager_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/control/ -v`
Expected: FAIL — `New`, `Manager` undefined.

- [ ] **Step 3: Implement**

Create `internal/control/manager.go`:

```go
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
```

Add the missing fields/methods. Note: `store.Store` is an interface; `time` import needed. `buildLocked` is the same component-construction code currently in `main.go`:

```go
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
```

Add helper methods (see produced interfaces above): `Config()`, `Pipeline`, `System`, `Apply` (locks, rebuild, rev++), `Rev()`, `KnownTypes()`, `Store()`, `Limiter()`, `buildOverlayRules()`, `copyPriority`. Full concrete code for these:

```go
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
```

`buildOverlayRules`:

```go
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
```

NOTE: `detector.RuleFromConfig` takes flat params (defined in Task 2) — `detector` cannot import `config` (config imports detector), so the flat-signature version is used here.

Add `cfgPath` field to the struct and `defaultConfigPath` + `copyPriority` implementations. `cfgPath` defaults to `"configs/config.yaml"` when only `WithConfig` is used (it is never read in that path, but keeps the field initialized):

```go
func defaultConfigPath() string { return "configs/config.yaml" }

func copyPriority(in map[detector.Type]int) map[detector.Type]int {
	out := make(map[detector.Type]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
```

Fix `Apply` fallback to restore the previous (valid) state on error:

```go
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
```

And `buildLocked` must assign `m.detector`, `m.context`, `m.whitelist`, `m.base`, `m.systems`, `m.limiter` (it does).

Also `New` should set `m.store` before `buildLocked` (it does).

- [ ] **Step 4: Run tests to verify they pass**

Add helper `mustManager` to the test file:

```go
func mustManager(t *testing.T) *Manager {
	t.Helper()
	cfg := config.Default()
	m, err := New(WithConfig(cfg), WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	return m
}
```

Add to `config` package a `WriteMinimal` helper used by the test:

```go
// WriteMinimal writes a minimal valid config file for tests.
func WriteMinimal(path string) error {
	return os.WriteFile(path, []byte("port: 8080\n"), 0o644)
}
```

Run: `go test ./internal/control/ ./internal/config/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/control/ internal/config/
git commit -m "control: runtime manager with atomic config reload and overlay rules"
```

---

### Task 5: Handler refactor — depend on Manager, expose /v1/config API

**Files:**
- Modify: `internal/api/handlers/process.go`, `internal/api/handlers/process_test.go`
- Create: `internal/api/handlers/config.go`, `internal/api/handlers/config_test.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Consumes (from Task 4): `control.New`, `(*Manager).Config`, `(*Manager).Pipeline(system)`, `(*Manager).System(name)`, `(*Manager).Apply`, `(*Manager).Limiter()`, `(*Manager).Store()`, `(*Manager).Rev()`, `(*Manager).KnownTypes()`, `cfg.Admin.Key`.
- Produces:
  - `handlers.Handler` reworked: replaces fields `Pipeline`, `Store`, `Cfg`, `Limiter` with `Mgr *control.Manager`.
  - `GET /v1/config` → JSON view: `{"rev", "masking", "systems":[{name, api_key_set, enabled, masking, allow_unmask, pii}], "rules":[{...}], "combinations":[{...}], "known_types":[...]}`.
  - `PUT /v1/config` → body = same JSON shape (plus optional `api_key` per system); requires `X-Admin-Key`; returns `{"rev": N}`; `400 {"error": "..."}` / `401` / `403`.
  - CORS middleware `handlers.CORS(origin string) func(http.Handler) http.Handler` on `/v1/*` and `/process`.
  - `GET /v1/config/rules` → `{"types": [...]}`.
  - In-request rate limiting and system auth: port `Limiter`, `systemConfig`, `authorize` to use `Mgr`.

- [ ] **Step 1: Write the failing test**

Create `internal/api/handlers/config_test.go`:

```go
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func newConfigHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(0, 1000)))
	require.NoError(t, err)
	h := &Handler{Mgr: m}
	return h
}

func TestGetConfigView(t *testing.T) {
	h := newConfigHandler(t)
	rec := httptest.NewRecorder()
	h.GetConfig(rec, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var view map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.Equal(t, float64(0), view["rev"])
	require.Contains(t, view, "systems")
	require.Contains(t, view, "known_types")
}

func TestPutConfigRequiresAdminKey(t *testing.T) {
	h := newConfigHandler(t)
	body, _ := json.Marshal(map[string]any{"masking": "token"})
	rec := httptest.NewRecorder()
	h.PutConfig(rec, httptest.NewRequest(http.MethodPut, "/v1/config", bytes.NewReader(body)))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestPutConfigApplies(t *testing.T) {
	h := newConfigHandler(t)
	body, _ := json.Marshal(map[string]any{"masking": "token"})
	req := httptest.NewRequest(http.MethodPut, "/v1/config", bytes.NewReader(body))
	req.Header.Set("X-Admin-Key", "pii-admin-key")
	rec := httptest.NewRecorder()
	h.PutConfig(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]uint64
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, uint64(1), resp["rev"])
}

func TestGetConfigRules(t *testing.T) {
	h := newConfigHandler(t)
	rec := httptest.NewRecorder()
	h.GetConfigRules(rec, httptest.NewRequest(http.MethodGet, "/v1/config/rules", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body["types"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/handlers/ -run 'TestGetConfig|TestPutConfig|TestGetConfigRules' -v`
Expected: FAIL — `Handler` has no `Mgr`, methods undefined. (Compile error is fine.)

- [ ] **Step 3: Implement**

Rework `internal/api/handlers/process.go` `Handler`:

```go
type Handler struct {
	Mgr *control.Manager
}
```

Imports in `process.go`: drop `pipeline`/`store`/`ratelimit` only if no longer referenced; `store.Store` methods are accessed via `h.Mgr.Store()` (returns `store.Store`), so the `store` import stays. `ratelimit` is no longer referenced in this file — remove it.

`systemPipeline`, `systemConfig`, `Process`, `authorize` become methods on this `Handler`, using `h.Mgr`:

```go
// systemPipeline returns the per-system pipeline. The second return value is
// true when the system resolves to a pipeline (enabled and known).
func (h *Handler) systemPipeline(name string) (*pipeline.Pipeline, bool) {
	if name == "" {
		return h.Mgr.Pipeline(""), true
	}
	s := h.Mgr.System(name)
	if s == nil || !s.Enabled {
		return nil, false
	}
	return h.Mgr.Pipeline(name), true
}

// systemConfig looks up a consumer system by name (nil if absent).
func (h *Handler) systemConfig(name string) *config.SystemConfig {
	return h.Mgr.System(name)
}

// authorize enforces the per-system API key via the X-API-Key header. Systems
// without a configured key are authorized implicitly.
func (h *Handler) authorize(r *http.Request, s *config.SystemConfig) error {
	if s == nil || s.APIKey == "" {
		return nil
	}
	if r.Header.Get("X-API-Key") == s.APIKey {
		return nil
	}
	return config.ErrUnauthorized
}
```

In `Process`:
- Replace `if h.Limiter != nil {` with `if lim := h.Mgr.Limiter(); lim != nil {` and `lim.Allow()`.
- Replace `allowUnmask := h.Cfg.AllowUnmask` / `h.Cfg.Systems` with:
  ```go
  allowUnmask := h.Mgr.Config().AllowUnmask
  if system != nil {
      allowUnmask = system.AllowUnmask
  }
  ```
- Replace `h.Store.Get(...)` with `h.Mgr.Store().Get(...)` and `h.Store.Save(...)` with `h.Mgr.Store().Save(...)`.
- Everything else (metrics, logging) stays identical.

Create `internal/api/handlers/config.go`:

```go
package handlers

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

// systemView is the JSON view of a system (no API key value, only a flag).
type systemView struct {
	Name        string          `json:"name"`
	APIKeySet   bool            `json:"api_key_set"`
	Enabled     bool            `json:"enabled"`
	Masking     string          `json:"masking"`
	AllowUnmask bool            `json:"allow_unmask"`
	PII         []detector.Type `json:"pii"`
}

// configView is the full config returned by GET /v1/config.
type configView struct {
	Rev       uint64        `json:"rev"`
	Masking   string        `json:"masking"`
	Systems   []systemView  `json:"systems"`
	Rules     []config.RuleConfig  `json:"rules"`
	Combinations []config.CombinationConfig `json:"combinations"`
	KnownTypes []string     `json:"known_types"`
}

// GetConfig handles GET /v1/config.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.Mgr.Config()
	view := configView{
		Rev:     h.Mgr.Rev(),
		Masking: cfg.Masking.Mode,
		Rules:   cfg.Rules,
		Combinations: cfg.Combinations,
		KnownTypes: h.Mgr.KnownTypes(),
	}
	for i := range cfg.Systems {
		s := &cfg.Systems[i]
		view.Systems = append(view.Systems, systemView{
			Name:        s.Name,
			APIKeySet:   s.APIKey != "",
			Enabled:     s.Enabled,
			Masking:     s.Masking,
			AllowUnmask: s.AllowUnmask,
			PII:         s.PII,
		})
	}
	writeJSON(w, view)
}

// PutConfig handles PUT /v1/config. Body is the same JSON shape as GET, plus
// optional per-system api_key (null/absent = unchanged, "" = cleared, value = set).
func (h *Handler) PutConfig(w http.ResponseWriter, r *http.Request) {
	if !adminAuthorized(r, h.Mgr.Config().Admin.Key) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var in struct {
		Masking string `json:"masking"`
		Systems []struct {
			Name        string          `json:"name"`
			APIKey      *string         `json:"api_key"`
			Enabled     *bool           `json:"enabled"`
			Masking     *string         `json:"masking"`
			AllowUnmask *bool           `json:"allow_unmask"`
			PII         []detector.Type `json:"pii"`
		} `json:"systems"`
		Rules        []config.RuleConfig       `json:"rules"`
		Combinations []config.CombinationConfig `json:"combinations"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
		return
	}
	cfg := h.mgrConfigSnapshot() // copy of the live config: patch it, then Apply
	if in.Masking != "" {
		cfg.Masking.Mode = in.Masking
	}
	for _, s := range in.Systems {
		idx := -1
		for i := range cfg.Systems {
			if cfg.Systems[i].Name == s.Name {
				idx = i
				break
			}
		}
		if idx == -1 {
			cfg.Systems = append(cfg.Systems, config.SystemConfig{Name: s.Name, Enabled: derefBool(s.Enabled, true)})
			idx = len(cfg.Systems) - 1
		}
		existing := &cfg.Systems[idx]
		if s.APIKey != nil {
			existing.APIKey = *s.APIKey
		}
		if s.Enabled != nil {
			existing.Enabled = *s.Enabled
		}
		if s.Masking != nil {
			existing.Masking = *s.Masking
		}
		if s.AllowUnmask != nil {
			existing.AllowUnmask = *s.AllowUnmask
		}
		if s.PII != nil {
			existing.PII = s.PII
		}
	}
	if in.Rules != nil {
		cfg.Rules = in.Rules
	}
	if in.Combinations != nil {
		cfg.Combinations = in.Combinations
	}
	if err := h.Mgr.Apply(cfg); err != nil {
		http.Error(w, `{"error": "`+err.Error()+`"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]uint64{"rev": h.Mgr.Rev()})
}

// GetConfigRules handles GET /v1/config/rules.
func (h *Handler) GetConfigRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string][]string{"types": h.Mgr.KnownTypes()})
}

func adminAuthorized(r *http.Request, key string) bool {
	if key == "" {
		return false
	}
	return r.Header.Get("X-Admin-Key") == key
}

func derefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// CORS returns middleware granting admin origin cross-origin access.
func CORS(origin string) func(http.Handler) http.Handler {
	originVal := origin
	if originVal == "" {
		originVal = "*"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", originVal)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Key, X-API-Key")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
```

`PutConfig` patches a copy of the live config via the `mgrConfigSnapshot` helper below (patching the live pointer would race with concurrent reads; Apply swaps a fresh, immutable config under the manager lock):

```go
// mgrConfigSnapshot returns a deep-enough copy of the current config for patching.
func (h *Handler) mgrConfigSnapshot() *config.Config {
	cfg := h.Mgr.Config()
	out := *cfg
	out.Systems = append([]config.SystemConfig(nil), cfg.Systems...)
	return &out
}
```

Metrics and logging unchanged. Also the `bytes` and `chi` imports are unused in config.go — remove them from the import list.

Wire routes in `cmd/server/main.go`. Remove the `ctxR`, `w`, `p := pipeline.New(...)`, `h := &handlers.Handler{...}` and limiter blocks (their construction moved into `control.Manager.buildLocked`). KEEP the NER loading block that populates `detectorOpts` (`detectorOpts := []detector.Option{}` … `detectorOpts = append(detectorOpts, detector.WithNER(m))`). Then replace the removed blocks with:

```go
mgr, err := control.New(
	control.WithConfig(cfg),
	control.WithStore(st),
	control.WithDetectorOpts(detectorOpts...),
)
if err != nil {
	slog.Error("failed to build pipeline", "error", err)
	os.Exit(1)
}
h := &handlers.Handler{Mgr: mgr}
```

Add routes after `/process`:

```go
r.Route("/v1", func(r chi.Router) {
	r.Use(handlers.CORS(adminOrigin()))
	r.Get("/config", h.GetConfig)
	r.Put("/config", h.PutConfig)
	r.Get("/config/rules", h.GetConfigRules)
})
r.With(handlers.CORS(adminOrigin())).Post("/process", h.Process)
```

Add helper:

```go
func adminOrigin() string {
	origin := os.Getenv("ADMIN_ORIGIN")
	if origin == "" {
		return "*"
	}
	return origin
}
```

Config type: the handler no longer needs pipeline.Config import for `SystemConfig`. Keep imports tidy: in `main.go` remove `pcontext`, `pipeline`, `ratelimit`, `whitelist` imports (their construction moved into `control`); add `control`; keep `detector`, `ner`, `store`, `redis`. In `process.go` remove `ratelimit`; keep `config`, `store`, `observability`, `pipeline` (used by `systemPipeline`).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/handlers/ ./internal/control/ ./internal/config/ -v`
Expected: PASS. Fix any existing `process_test.go` compilation: it references `h.Pipeline`, `h.Cfg`, `h.Store`, `h.Limiter`. Update those to use manager:

In `process_test.go` replace the helper and adapt the mutation-based tests as below.

Make the helper accept optional config mutations, so system-dependent tests build the manager with their system upfront:

```go
func newTestHandler(t *testing.T, mutate ...func(*config.Config)) *Handler {
	t.Helper()
	cfg := config.Default()
	for _, fn := range mutate {
		fn(cfg)
	}
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	return &Handler{Mgr: m}
}
```

Then migrate the tests that mutated `h.Cfg`/`h.Limiter`/`h.Pipeline`:
- `TestProcessTokenModeMasksAndUnmasks`: call `h := newTestHandler(t, func(c *config.Config) { c.Masking.Mode = "token" })`; DELETE the `h.Cfg.Masking.Mode = "token"` + `h.Pipeline = pipeline.New(...)` rebuild block.
- `TestProcessSensitiveCooccurrence`: `newTestHandler(t, func(c *config.Config) { c.SensitiveTypes = []detector.Type{detector.TypePIN, detector.TypeCVV} })`, drop the `h.Cfg.SensitiveTypes = ...` line.
- `TestProcessRateLimit`: `newTestHandler(t, func(c *config.Config) { c.RateLimit = config.RateLimitConfig{RPS: 2, Burst: 2} })`, drop `h.Limiter = ratelimit.New(2, 2)`.
- `TestProcessSystemAllowlistTypes` / `TestProcessSystemNoUnmask` / `TestProcessSystemDisabled` / `TestProcessSystemUnknown` / `TestProcessSystemAPIKey`: build the system via the mutate func, e.g. `newTestHandler(t, func(c *config.Config) { c.Systems = []config.SystemConfig{{Name: "analytics", Enabled: true, PII: []detector.Type{detector.TypePhone, detector.TypeEmail}, AllowUnmask: false}} })`, drop the `h.Cfg.Systems = ...` line.
- `TestProcessRedisDown`: needs a broken redis store, so build the manager explicitly (the store cannot be swapped post-construction):
  ```go
  mr := miniredis.RunT(t)
  client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
  m, err := control.New(
      control.WithConfig(config.Default()),
      control.WithStore(store.NewRedis(client, time.Hour)),
  )
  require.NoError(t, err)
  h := &Handler{Mgr: m}
  mr.Close()
  ```
  and drop the `h.Store = store.NewRedis(...)` line.

Update the import list in the test file: add `control`, drop `resolve`/`context`/`whitelist`/`ratelimit`/`pipeline` if no longer referenced (the `pipeline` import may disappear entirely once the rebuild block is deleted). Keep `redis`, `store`, `config`, `detector`.

- [ ] **Step 5: Run full test suite**

Run: `go build ./... && go test ./...`
Expected: ALL PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/ cmd/server/
git commit -m "api: runtime config endpoints (GET/PUT /v1/config), CORS, manager-backed handler"
```

---

### Task 6: Default config + README section for admin config

**Files:**
- Modify: `configs/config.yaml`, `README.md`

**Interfaces:**
- Produces: default `config.yaml` with `admin:` key and a sample overlay rule + combination (commented-out) demonstrating the feature.

- [ ] **Step 1: Modify config.yaml**

Append to `configs/config.yaml`:

```yaml
admin:
  key: "pii-admin-key"

# Правила расширения (overlay): новые типы ПДН без переписывания ядра.
# Требуют: type (имя), regex (go RE2) или capture (значение в group 1).
rules:
  - type: "СНИЛС"
    regex: "\\d{3}-\\d{3}-\\d{3}\\s\\d{2}"
    priority: 0
    confidence: 0.99

# Комбинации: тип маскируется только при наличии требуемых типов рядом.
# Раскомментируйте, чтобы ПИН маскировался только рядом с картой.
# combinations:
#   - type: "ПИН"
#     requires: ["КАРТА"]
#     window: 80
```

- [ ] **Step 2: Modify README**

Add a section "Live-настройка (hot-reload) через admin API" describing:
- `GET /v1/config` (view, no keys), `PUT /v1/config` (`X-Admin-Key`), `GET /v1/config/rules`.
- Example curl enabling PIN→card combination.
- Admin UI in `web/` reference.

- [ ] **Step 3: Verify**

Run: `go build ./...` and `go test ./...` — unchanged green.

- [ ] **Step 4: Commit**

```bash
git add configs/config.yaml README.md
git commit -m "docs: default overlay rule example and admin API section"
```

---

### Task 7: React SPA scaffold (Vite + TanStack Query + TelegramUI)

**Files:**
- Create: `web/package.json`, `web/vite.config.ts`, `web/tsconfig.json`, `web/index.html`, `web/src/main.tsx`, `web/src/App.tsx`, `web/src/api.ts`
- Create: `web/.gitignore`

**Interfaces:**
- Consumes: `PUT /v1/config` (JSON), `GET /v1/config`, `GET /v1/config/rules`; admin key via `X-Admin-Key` header.
- Produces: TypeScript types and query hooks in `api.ts`; a tabbed `App` with five sections (placeholders rendered by Tasks 8–9).

- [ ] **Step 1: Write package.json**

```json
{
  "name": "pii-admin",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc --noEmit && vite build",
    "preview": "vite preview"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.0.0",
    "@telegram-apps/telegram-ui": "^2.1.13",
    "react": "^18.3.1",
    "react-dom": "^18.3.1"
  },
  "devDependencies": {
    "@types/react": "^18.3.1",
    "@types/react-dom": "^18.3.1",
    "@vitejs/plugin-react": "^4.3.1",
    "typescript": "^5.5.0",
    "vite": "^5.4.0"
  }
}
```

- [ ] **Step 2: Write vite.config.ts**

```ts
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/v1': 'http://localhost:8080',
      '/process': 'http://localhost:8080',
    },
  },
  build: { outDir: 'dist' },
});
```

- [ ] **Step 3: Write tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "skipLibCheck": true,
    "esModuleInterop": true,
    "noEmit": true,
    "types": ["vite/client"]
  },
  "include": ["src"]
}
```

- [ ] **Step 4: Write index.html**

```html
<!doctype html>
<html lang="ru">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>PII Gateway — Admin</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 5: Write src/api.ts**

```ts
export interface SystemInfo {
  name: string;
  api_key_set: boolean;
  enabled: boolean;
  masking: string;
  allow_unmask: boolean;
  pii: string[];
}

export interface RuleInfo {
  type: string;
  regex: string;
  priority: number;
  context: string;
  capture: string;
  keyword: string;
  confidence: number;
}

export interface CombinationInfo {
  type: string;
  requires: string[];
  window: number;
}

export interface ConfigView {
  rev: number;
  masking: string;
  systems: SystemInfo[];
  rules: RuleInfo[];
  combinations: CombinationInfo[];
  known_types: string[];
}

export interface PutConfigBody {
  masking?: string;
  systems?: Array<Partial<SystemInfo> & { api_key?: string | null }>;
  rules?: RuleInfo[];
  combinations?: CombinationInfo[];
}

let adminKey = '';

export function setAdminKey(k: string) {
  adminKey = k;
}

export function getAdminKey() {
  return adminKey;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(init?.headers as Record<string, string>),
  };
  if (adminKey && ['PUT', 'POST'].includes(init?.method ?? '')) {
    headers['X-Admin-Key'] = adminKey;
  }
  const res = await fetch(path, { ...init, headers });
  if (!res.ok) {
    const text = await res.text();
    throw new Error(`${res.status}: ${text.slice(0, 200)}`);
  }
  return (await res.json()) as T;
}

export const api = {
  getConfig: () => request<ConfigView>('/v1/config'),
  getRules: () => request<{ types: string[] }>('/v1/config/rules'),
  putConfig: (body: PutConfigBody) => request<{ rev: number }>('/v1/config', { method: 'PUT', body: JSON.stringify(body) }),
};
```

- [ ] **Step 6: Write src/main.tsx**

```tsx
import React from 'react';
import ReactDOM from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import '@telegram-apps/telegram-ui/dist/styles.css';
import { AppRoot } from '@telegram-apps/telegram-ui';
import App from './App';

const queryClient = new QueryClient();

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <AppRoot>
        <App />
      </AppRoot>
    </QueryClientProvider>
  </React.StrictMode>,
);
```

- [ ] **Step 7: Write src/App.tsx (tabs shell)**

```tsx
import { useState } from 'react';
import { Cell, List, Section } from '@telegram-apps/telegram-ui';
import SystemsTab from './SystemsTab';
import RulesTab from './RulesTab';
import CombinationsTab from './CombinationsTab';
import ConfigTab from './ConfigTab';

type Tab = 'systems' | 'rules' | 'combinations' | 'config';

const TABS: Array<{ id: Tab; label: string }> = [
  { id: 'systems', label: 'Системы' },
  { id: 'rules', label: 'Правила ПДН' },
  { id: 'combinations', label: 'Комбинации' },
  { id: 'config', label: 'Конфиг' },
];

export default function App() {
  const [tab, setTab] = useState<Tab>('systems');
  return (
    <div style={{ maxWidth: 720, margin: '0 auto', padding: 16 }}>
      <Section header="PII Gateway — Админка">
        <List>
          {TABS.map((t) => (
            <Cell key={t.id} subtitle={tab === t.id ? 'активен' : undefined} onClick={() => setTab(t.id)}>
              {t.label}
            </Cell>
          ))}
        </List>
      </Section>
      {tab === 'systems' && <SystemsTab />}
      {tab === 'rules' && <RulesTab />}
      {tab === 'combinations' && <CombinationsTab />}
      {tab === 'config' && <ConfigTab />}
    </div>
  );
}
```

- [ ] **Step 8: Install and typecheck**

Run: `cd web && npm install`
Run: `cd web && npx tsc --noEmit`
Expected: typecheck fails — SystemsTab/RulesTab/CombinationsTab/ConfigTab not yet created (Tasks 8–9). That is expected; install a placeholder `export default () => null` for each now:

Create minimal stubs temporarily, or better: create them fully in Task 8. For this task, add stubs so the scaffold compiles:

```tsx
// src/SystemsTab.tsx etc.
export default function Placeholder() {
  return <div>…</div>;
}
```

Run again `npx tsc --noEmit` → expected PASS.

- [ ] **Step 9: Commit**

```bash
cd web
git add web/
git commit -m "web: vite react-query telegram-ui scaffold with config API client"
```

---

### Task 8: React components — Systems and Rules tabs

**Files:**
- Create: `web/src/SystemsTab.tsx`, `web/src/RulesTab.tsx`, `web/src/AdminKeyInput.tsx`

**Interfaces:**
- Consumes: `api.getConfig`, `api.putConfig`, `getAdminKey`, `setAdminKey`; types `ConfigView`, `SystemInfo`, `RuleInfo`.
- Produces: `SystemsTab` (list systems, toggle enabled, show allow_unmask/pii chips, admin key input), `RulesTab` (CRUD overlay rules).

- [ ] **Step 1: Write AdminKeyInput.tsx**

```tsx
import { Input } from '@telegram-apps/telegram-ui';
import { getAdminKey, setAdminKey } from './api';

export default function AdminKeyInput() {
  return (
    <Input
      title="Admin-ключ"
      placeholder="X-Admin-Key"
      defaultValue={getAdminKey()}
      onChange={(e) => setAdminKey(e.target.value)}
    />
  );
}
```

- [ ] **Step 2: Write SystemsTab.tsx**

```tsx
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Cell, Checkbox, Input, List, Section, Switch } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type SystemInfo } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function SystemsTab() {
  const qc = useQueryClient();
  const { data: cfg, isLoading } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const save = useMutation({
    mutationFn: (systems: SystemInfo[]) => api.putConfig({ systems }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  if (isLoading) return <div>Загрузка…</div>;
  if (!cfg) return <div>Нет данных</div>;

  const update = (i: number, patch: Partial<SystemInfo>) => {
    const next = [...(cfg.systems ?? [])];
    next[i] = { ...next[i], ...patch };
    save.mutate(next);
  };

  return (
    <Section header="Системы-потребители">
      <AdminKeyInput />
      <List>
        {(cfg.systems ?? []).map((s, i) => (
          <Section key={s.name} header={s.name}>
            <Cell
              subtitle={`allow_unmask: ${s.allow_unmask ? 'да' : 'нет'} · режим: ${s.masking}`}
              after={<Switch checked={s.enabled} onChange={(e) => update(i, { enabled: e.target.checked })} />}
            >
              {s.enabled ? 'Включена' : 'Отключена'}
            </Cell>
            <Cell subtitle="Разрешённые типы ПДН">
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                {(s.pii ?? []).map((t) => (
                  <span key={t}>{t}</span>
                ))}
              </div>
            </Cell>
          </Section>
        ))}
      </List>
      {save.isError && <div style={{ color: 'red' }}>Ошибка: {String(save.error)}</div>}
      {save.isSuccess && <div style={{ color: 'green' }}>Сохранено (rev {save.data.rev})</div>}
    </Section>
  );
}
```

- [ ] **Step 3: Write RulesTab.tsx**

```tsx
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type ConfigView, type RuleInfo } from './api';
import AdminKeyInput from './AdminKeyInput';

const emptyRule: RuleInfo = { type: '', regex: '', priority: 0, context: '', capture: '', keyword: '', confidence: 0.99 };

export default function RulesTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [draft, setDraft] = useState<RuleInfo>(emptyRule);
  const saveRules = useMutation({
    mutationFn: (rules: RuleInfo[]) => api.putConfig({ rules }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  const addRule = () => {
    const rules = [...(cfg?.rules ?? []), draft];
    saveRules.mutate(rules);
    setDraft(emptyRule);
  };

  return (
    <Section header="Overlay-правила ПДН (добавляет типы без переписывания ядра)">
      <AdminKeyInput />
      <List>
        {(cfg?.rules ?? []).map((r) => (
          <Section key={r.type} header={`${r.type} · conf ${r.confidence}`}>
            <div>{r.regex}</div>
            {r.context && <div>контекст: {r.context}</div>}
          </Section>
        ))}
      </List>
      <Section header="Новое правило">
        <Input title="Тип" value={draft.type} onChange={(e) => setDraft({ ...draft, type: e.target.value })} placeholder="СНИЛС" />
        <Input title="Regex (RE2)" value={draft.regex} onChange={(e) => setDraft({ ...draft, regex: e.target.value })} />
        <Input title="Контекст (regex)" value={draft.context} onChange={(e) => setDraft({ ...draft, context: e.target.value })} />
        <Input title="Confidence" type="number" step="0.01" value={String(draft.confidence)} onChange={(e) => setDraft({ ...draft, confidence: Number(e.target.value) })} />
        <Button onClick={addRule}>Добавить правило</Button>
      </Section>
      {saveRules.isError && <div style={{ color: 'red' }}>{String(saveRules.error)}</div>}
    </Section>
  );
}
```

- [ ] **Step 4: Remove the stub for SystemsTab/RulesTab**

Delete the placeholder stubs created in Task 7 for these two files (keep stubs for CombinationsTab/ConfigTab until Task 9).

- [ ] **Step 5: Typecheck + build**

Run: `cd web && npx tsc --noEmit`
Expected: PASS. (CombinationsTab/ConfigTab stubs still exist.)
Run: `cd web && npm run build`
Expected: dist/ produced.

- [ ] **Step 6: Commit**

```bash
cd web
git add web/src/
git commit -m "web: systems and overlay-rules admin tabs"
```

---

### Task 9: React components — Combinations tab, Config (raw JSON) tab

**Files:**
- Create: `web/src/CombinationsTab.tsx`, `web/src/ConfigTab.tsx`
- Delete: stub files for these two.

**Interfaces:**
- Consumes: `api.putConfig`, `api.getConfig`, types; `known_types` for pickers.
- Produces: combinations CRUD; raw JSON editor that round-trips the whole `ConfigView` minus `rev`/`known_types`.

- [ ] **Step 1: Write CombinationsTab.tsx**

```tsx
import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Input, List, Section } from '@telegram-apps/telegram-ui';
import { api, type CombinationInfo, type ConfigView } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function CombinationsTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [type, setType] = useState('ПИН');
  const [requires, setRequires] = useState('КАРТА');
  const [window, setWindow] = useState('80');
  const save = useMutation({
    mutationFn: (combinations: CombinationInfo[]) => api.putConfig({ combinations }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  const add = () => {
    const next = [...(cfg?.combinations ?? []), { type, requires: requires.split(',').map((s) => s.trim()).filter(Boolean), window: Number(window) }];
    save.mutate(next);
  };

  return (
    <Section header="Комбинации типов (маскировать только вместе)">
      <AdminKeyInput />
      <List>
        {(cfg?.combinations ?? []).map((c) => (
          <Section key={`${c.type}-${c.requires.join('+')}`} header={c.type}>
            <div>требует: {c.requires.join(', ')} · window {c.window}</div>
          </Section>
        ))}
      </List>
      <Section header="Новая комбинация">
        <Input title="Тип (маскируется)" value={type} onChange={(e) => setType(e.target.value)} placeholder="ПИН" />
        <Input title="Требуемые типы (через запятую)" value={requires} onChange={(e) => setRequires(e.target.value)} placeholder="КАРТА" />
        <Input title="Window (байт)" value={window} onChange={(e) => setWindow(e.target.value)} />
        <Button onClick={add}>Добавить комбинацию</Button>
      </Section>
      {save.isError && <div style={{ color: 'red' }}>{String(save.error)}</div>}
    </Section>
  );
}
```

- [ ] **Step 2: Write ConfigTab.tsx**

```tsx
import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Button, Section } from '@telegram-apps/telegram-ui';
import { api, getAdminKey, setAdminKey, type ConfigView } from './api';
import AdminKeyInput from './AdminKeyInput';

export default function ConfigTab() {
  const qc = useQueryClient();
  const { data: cfg } = useQuery<ConfigView>({ queryKey: ['config'], queryFn: api.getConfig });
  const [text, setText] = useState('');
  const [key, setKey] = useState(getAdminKey());
  const apply = useMutation({
    mutationFn: (body: string) => api.putConfig(JSON.parse(body)),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['config'] }),
  });

  useEffect(() => {
    if (cfg) {
      const { rev: _rev, known_types: _kt, ...rest } = cfg;
      setText(JSON.stringify({ ...rest, systems: (rest.systems ?? []).map((s) => ({ ...s, api_key: null })) }, null, 2));
    }
  }, [cfg]);

  return (
    <Section header="Конфиг (JSON)">
      <AdminKeyInput />
      <textarea
        style={{ width: '100%', minHeight: 400, fontFamily: 'monospace', fontSize: 12 }}
        value={text}
        onChange={(e) => setText(e.target.value)}
      />
      <Button mode="filled" onClick={() => { if (key) setAdminKey(key); apply.mutate(text); }} disabled={apply.isPending}>
        Применить (PUT /v1/config)
      </Button>
      {apply.isSuccess && <div style={{ color: 'green' }}>Применено, rev={apply.data.rev}</div>}
      {apply.isError && <div style={{ color: 'red' }}>{String(apply.error)}</div>}
    </Section>
  );
}
```

- [ ] **Step 3: Remove stubs and typecheck**

Delete `CombinationsTab`/`ConfigTab` stub files. 
Run: `cd web && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 4: Build + manual dev check**

Run: `cd web && npm run build`
Expected: dist/ produced.

Manual: with the Go server running on :8080, `npm run dev`, open http://localhost:5173, check the four tabs render live data and Apply works (admin key `pii-admin-key`).

- [ ] **Step 5: Commit**

```bash
cd web
git add web/src/
git commit -m "web: combinations and raw config editor tabs"
```

---

### Task 10: Docker for admin UI (nginx) + compose wiring

**Files:**
- Create: `web/Dockerfile`, `web/nginx.conf`, `web/.dockerignore`
- Modify: `docker-compose.yml`

**Interfaces:**
- Produces: image serving the SPA at `/` and proxying `/v1` and `/process` to the Go service (a `pii-module-v2:8080` alias), plus a docker-compose service `admin`.

- [ ] **Step 1: Write web/Dockerfile**

```dockerfile
# syntax=docker/dockerfile:1
FROM node:20-alpine AS build
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm install
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
```

- [ ] **Step 2: Write web/nginx.conf**

```nginx
server {
    listen 80;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    location /v1/ {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Admin-Key $http_x_admin_key;
    }

    location /process {
        proxy_pass http://pii-module-v2:8080;
        proxy_set_header Host $host;
    }

    location / {
        try_files $uri /index.html;
    }
}
```

- [ ] **Step 3: Write web/.dockerignore**

```
node_modules
dist
```

- [ ] **Step 4: Update docker-compose.yml**

Add service:

```yaml
  admin:
    build: ./web
    ports:
      - "5173:80"
    depends_on:
      - pii-module-v2
    restart: unless-stopped
```

- [ ] **Step 5: Validate compose file**

Run: `docker compose config`
Expected: valid YAML. (Docker daemon may be unavailable; `docker compose config` may still fail without a daemon. If so, validate YAML with `python3 -c "import yaml,sys; yaml.safe_load(open('docker-compose.yml'))"`.)

- [ ] **Step 6: Commit**

```bash
git add web/Dockerfile web/nginx.conf web/.dockerignore docker-compose.yml
git commit -m "docker: nginx admin UI with proxy to gateway; compose admin service"
```

---

### Task 11: End-to-end verification + demo flow

**Files:**
- Verify only (no new files unless a fix is needed).

- [ ] **Step 1: Build and run all Go tests**

Run: `go build ./... && go test ./...`
Expected: ALL PASS.

- [ ] **Step 2: Rebuild and restart the gateway**

Run: `go build -o /tmp/pii-serverln ./cmd/server`
Run: `pkill -f pii-serverln || true; nohup /tmp/pii-serverln -config configs/config.yaml > /tmp/pii-server.log 2>&1 &`

- [ ] **Step 3: Verify config endpoints over HTTP**

Run:
```bash
curl -s localhost:8080/v1/config | python3 -m json.tool | head -40
curl -s localhost:8080/v1/config/rules
```
Expected: JSON with `systems`, `known_types` (incl. `СНИЛС` from overlay), types list.

- [ ] **Step 4: PUT a combination and verify behavior**

Run:
```bash
curl -s -X PUT localhost:8080/v1/config -H 'X-Admin-Key: pii-admin-key' \
  -H 'Content-Type: application/json' \
  -d '{"combinations":[{"type":"ПИН","requires":["КАРТА"],"window":80}]}'
```
Expected: `{"rev":1}`.

Then verify masking behavior (rev must be >0 before this):
```bash
curl -s -X POST localhost:8080/process -d '{"payload":"пин 1234","payload_id":"cdemo1"}'
```
Expected: `{"result":"пин 1234"}` (PIN not masked without card).
```bash
curl -s -X POST localhost:8080/process -d '{"payload":"карта 4276 1234 5678 9012, пин 1234","payload_id":"cdemo2"}'
```
Expected: both `[КАРТА]` and `[ПИН]` masked.

- [ ] **Step 5: Verify admin UI**

Run: `cd web && npm install && npm run dev` (or use the built image on :5173).
Open http://localhost:5173 in a browser, switch tabs, confirm systems/rules render and Apply round-trips.

- [ ] **Step 6: Verify logs/metrics still OK**

Run:
```bash
curl -s localhost:8080/health
curl -s localhost:8080/metrics | grep -E 'pii_requests_total|pii_latency_seconds'
```
Expected: healthy; counters present.

- [ ] **Step 7: Final commit (if any fixes)**

```bash
git add -A
git commit -m "chore: admin UI + runtime config fixes from e2e verification"
```

---

## Self-Review

**Spec coverage:**
- Hot-reload via `PUT /v1/config` and atomic `Manager.Apply` → Tasks 4–5.
- JSON-only config API → Task 5 (dedicated payload/view structs, no direct Config marshalling).
- Admin key guard → Task 1 (`config.Admin.Key`), Task 5 (`adminAuthorized`).
- Overlay rules (new PII types) → Tasks 2, 4; UI CRUD → Task 8.
- Combinations (PIN→CARD) → Task 3; UI CRUD → Task 9.
- Known types endpoint → Tasks 5 (GetConfigRules).
- api_key round-trip (`null`/`""`/value) → Task 5 `PutConfig` handling.
- CORS + separate React service → Tasks 5, 7–9, 10.
- NER survives reload → `control.WithDetectorOpts` re-applies detector options (incl. `WithNER`) on every rebuild; the NER model instance is loaded once in `main.go` and passed into the manager. This matches the spec.
- TelegramUI → Task 7 deps + usage.
- Vite dev + nginx prod → Task 10.
- README admin/hot-reload docs → Task 6.
- Rate limiter rebuilt per Apply → Task 4 `buildLocked` (`limiter`).
- Tests + types → Tasks 1–5 include unit tests; Task 8–9 typecheck/build.

**Placeholder scan:** All steps contain concrete code. No TODOs. Import-cycle edge (`detector` cannot import `config`) is resolved by the flat-param signature of `RuleFromConfig`; `buildOverlayRules` lives in `control` and maps `config.RuleConfig` → flat args.

**Type consistency check:**
- `control.New(WithConfig, WithStore, WithConfigPath)` matches test usage.
- `Handler{Mgr: *control.Manager}` replaces `Handler{Pipeline, Store, Cfg, Limiter}` everywhere.
- `detector.RuleFromConfig(type, regex, context, capture, priority, confidence, keyword)` — keyword added last; ensure ordering matches call site in `buildOverlayRules`.
- `Pipeline.Options.Combinations []Combination` used by `buildLocked`.
- `api.putConfig` typed `PutConfigBody`; `ConfigView` fields match server `configView` JSON tags.
- `Combination` window default 80 mirrors resolution window.