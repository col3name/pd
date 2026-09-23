package handlers

import (
	"encoding/json"
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
	Rev          uint64                     `json:"rev"`
	Masking      string                     `json:"masking"`
	Systems      []systemView               `json:"systems"`
	Rules        []config.RuleConfig        `json:"rules"`
	Combinations []config.CombinationConfig `json:"combinations"`
	KnownTypes   []string                   `json:"known_types"`
}

// GetConfig handles GET /v1/config.
func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.Mgr.Config()
	view := configView{
		Rev:          h.Mgr.Rev(),
		Masking:      cfg.Masking.Mode,
		Rules:        cfg.Rules,
		Combinations: cfg.Combinations,
		KnownTypes:   h.Mgr.KnownTypes(),
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

// GetConfigRules handles GET /v1/config/rules.
func (h *Handler) GetConfigRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string][]string{"types": h.Mgr.KnownTypes()})
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
