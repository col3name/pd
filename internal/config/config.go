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
	return cfg, nil
}