package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/resolve"
)

// ErrUnauthorized is returned when a system API key does not match.
var ErrUnauthorized = errors.New("unauthorized")

// MaskingConfig controls redact/token behavior.
type MaskingConfig struct {
	Mode string `yaml:"mode"` // "redact" | "token"
}

// StoreConfig controls the payload store.
type StoreConfig struct {
	Type     string `yaml:"type"` // "memory" | "redis"
	TTLHours int    `yaml:"ttl_hours"`
	Capacity int    `yaml:"capacity"`
	RedisURL string `yaml:"redis_url"`
}

// ContextConfig tunes the context resolver keywords (nil maps → defaults).
type ContextConfig struct {
	Enabled bool                       `yaml:"enabled"`
	Boost   map[detector.Type][]string `yaml:"boost"`
	Penalty map[detector.Type][]string `yaml:"penalty"`
}

// WhitelistConfig lists non-PII entries.
type WhitelistConfig struct {
	Enabled       bool     `yaml:"enabled"`
	Persons       []string `yaml:"persons"`
	Addresses     []string `yaml:"addresses"`
	Organizations []string `yaml:"organizations"`
}

// ResolveConfig holds type priorities.
type ResolveConfig struct {
	Priority map[detector.Type]int `yaml:"priority"`
}

// MLConfig controls the optional NER model.
type MLConfig struct {
	Enabled   bool     `yaml:"enabled"`
	ModelPath string   `yaml:"model_path"`
	VocabPath string   `yaml:"vocab_path"`
	Labels    []string `yaml:"labels"`
}

// RateLimitConfig controls the token-bucket limiter (0 disables).
type RateLimitConfig struct {
	RPS   float64 `yaml:"rps"`
	Burst float64 `yaml:"burst"`
}

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

// SystemConfig describes a consumer system: which PII types to mask, the
// masking mode, and whether unmasking is allowed for that system.
type SystemConfig struct {
	Name        string          `yaml:"name"`
	APIKey      string          `yaml:"api_key"` // non-empty => require X-API-Key
	Enabled     bool            `yaml:"enabled"` // false => system rejected (403)
	PII         []detector.Type `yaml:"pii"`     // empty => all supported types
	Masking     string          `yaml:"masking"` // "redact" | "token"; empty => global
	AllowUnmask bool            `yaml:"allow_unmask"`
}

// Config is the version2 runtime configuration.
type Config struct {
	Port           int                 `yaml:"port"`
	AllowUnmask    bool                `yaml:"allow_unmask"`
	Store          StoreConfig         `yaml:"store"`
	Masking        MaskingConfig       `yaml:"masking"`
	Context        ContextConfig       `yaml:"context"`
	Whitelist      WhitelistConfig     `yaml:"whitelist"`
	Resolve        ResolveConfig       `yaml:"resolve"`
	ML             MLConfig            `yaml:"ml"`
	RateLimit      RateLimitConfig     `yaml:"rate_limit"`
	SensitiveTypes []detector.Type     `yaml:"sensitive"`
	Systems        []SystemConfig      `yaml:"systems"`
	Admin          AdminConfig         `yaml:"admin"`
	Rules          []RuleConfig        `yaml:"rules"`
	Combinations   []CombinationConfig `yaml:"combinations"`
}

// Default returns the v1-compatible default configuration.
func Default() *Config {
	// Deep-copy the package-global priority map: yaml.Unmarshal writes into an
	// existing map in place, so sharing resolve.DefaultPriority would corrupt
	// the global used by resolve.Resolve and every subsequent Default().
	priority := make(map[detector.Type]int, len(resolve.DefaultPriority))
	for k, v := range resolve.DefaultPriority {
		priority[k] = v
	}
	return &Config{
		Port:           8080,
		AllowUnmask:    true,
		Store:          StoreConfig{Type: "memory", TTLHours: 24, Capacity: 1 << 18},
		Masking:        MaskingConfig{Mode: "redact"},
		Context:        ContextConfig{Enabled: true},
		Whitelist:      WhitelistConfig{Enabled: true},
		Resolve:        ResolveConfig{Priority: priority},
		SensitiveTypes: []detector.Type{detector.TypePIN, detector.TypeCVV},
		Admin:          AdminConfig{Key: "pii-admin-key"},
	}
}

// Load reads the YAML file at path on top of Default() and applies env overrides.
func Load(path string) (*Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	applyEnv(cfg)
	return cfg, nil
}

// WriteMinimal writes a minimal valid config file for tests.
func WriteMinimal(path string) error {
	return os.WriteFile(path, []byte("port: 8080\n"), 0o644)
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("PORT"); v != "" {
		var p int
		if _, err := fmt.Sscanf(v, "%d", &p); err == nil {
			cfg.Port = p
		}
	}
	if v := os.Getenv("STORE"); v != "" {
		cfg.Store.Type = v
	}
	if v := os.Getenv("MASK_MODE"); v != "" {
		cfg.Masking.Mode = v
	}
}
