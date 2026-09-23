package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/kind-earthquake/pii-module/internal/config"
)

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
	systems, err := h.Repo.ListSystems(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	view := configView{
		Rev:        h.Mgr.Rev(),
		Masking:    cfg.Masking.Mode,
		KnownTypes: h.Mgr.KnownTypes(),
	}
	for _, s := range systems {
		view.Systems = append(view.Systems, toView(s))
	}
	rules, _ := h.Repo.ListRules(r.Context())
	combos, _ := h.Repo.ListCombinations(r.Context())
	view.Rules = rules
	view.Combinations = combos
	writeJSON(w, view)
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