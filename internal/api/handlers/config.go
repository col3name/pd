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
		Rules        []config.RuleConfig        `json:"rules"`
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
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]uint64{"rev": h.Mgr.Rev()})
}

// GetConfigRules handles GET /v1/config/rules.
func (h *Handler) GetConfigRules(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string][]string{"types": h.Mgr.KnownTypes()})
}

// mgrConfigSnapshot returns a deep-enough copy of the current config for patching.
func (h *Handler) mgrConfigSnapshot() *config.Config {
	cfg := h.Mgr.Config()
	out := *cfg
	out.Systems = append([]config.SystemConfig(nil), cfg.Systems...)
	return &out
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
