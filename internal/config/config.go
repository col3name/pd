package config

import (
	"fmt"
	"os"
	"time"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// Config holds module configuration loaded from environment.
type Config struct {
	Port        int
	RedisURL    string
	StoreTTL    time.Duration
	AllowUnmask bool
	MaskTypes   []detector.Type
	// SensitiveTypes are masked only when another PII type is also present
	// (co-occurrence rule). A lone sensitive value (e.g. a PIN without a card
	// number) is left unmasked.
	SensitiveTypes []detector.Type
	// RateLimitRPS is the max requests per second before 429 is returned.
	// 0 disables rate limiting.
	RateLimitRPS float64
	// RateLimitBurst is the token-bucket burst capacity.
	RateLimitBurst float64
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// Load returns configuration with defaults and environment overrides.
func Load() (*Config, error) {
	cfg := &Config{}
	cfg.Port = 8080
	if v := os.Getenv("PORT"); v != "" {
		var p int
		if _, err := fmt.Sscanf(v, "%d", &p); err == nil {
			cfg.Port = p
		}
	}
	cfg.RedisURL = env("REDIS_URL", "redis://localhost:6379/0")
	cfg.StoreTTL = 24 * time.Hour
	if v := os.Getenv("STORE_TTL_HOURS"); v != "" {
		var h int
		if _, err := fmt.Sscanf(v, "%d", &h); err == nil && h > 0 {
			cfg.StoreTTL = time.Duration(h) * time.Hour
		}
	}
	cfg.AllowUnmask = env("ALLOW_UNMASK", "true") != "false"
	cfg.MaskTypes = []detector.Type{
		detector.TypeFIO, detector.TypeBirthDate, detector.TypeBirthPlace,
		detector.TypePassport, detector.TypeCitizenship, detector.TypeIssuer,
		detector.TypeDeptCode, detector.TypePassportIssue, detector.TypeDriverLicense,
		detector.TypeAddress, detector.TypeEmail, detector.TypePhone,
		detector.TypeINN, detector.TypeCard, detector.TypeCVV,
		detector.TypePIN, detector.TypeCardholder,
	}
	// PIN and CVV are masked only when another PII type is present.
	cfg.SensitiveTypes = []detector.Type{detector.TypePIN, detector.TypeCVV}
	// Rate limiting: default 0 (disabled). Enable via RATE_LIMIT_RPS.
	cfg.RateLimitRPS = envFloat("RATE_LIMIT_RPS", 0)
	cfg.RateLimitBurst = envFloat("RATE_LIMIT_BURST", cfg.RateLimitRPS)
	return cfg, nil
}

func envFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var f float64
	if _, err := fmt.Sscanf(v, "%f", &f); err == nil && f > 0 {
		return f
	}
	return fallback
}