package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
)

// systemView is the JSON view of a system (no key value, only a flag).
type systemView struct {
	Name        string          `json:"name"`
	APIKeySet   bool            `json:"api_key_set"`
	Enabled     bool            `json:"enabled"`
	Masking     string          `json:"masking"`
	AllowUnmask bool            `json:"allow_unmask"`
	PII         []detector.Type `json:"pii"`
}

func toView(s config.SystemConfig) systemView {
	return systemView{
		Name:        s.Name,
		APIKeySet:   s.APIKey != "",
		Enabled:     s.Enabled,
		Masking:     s.Masking,
		AllowUnmask: s.AllowUnmask,
		PII:         s.PII,
	}
}

// ListSystems handles GET /v1/systems.
func (h *Handler) ListSystems(w http.ResponseWriter, r *http.Request) {
	systems, err := h.Repo.ListSystems(r.Context())
	if err != nil {
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	out := make([]systemView, 0, len(systems))
	for _, s := range systems {
		out = append(out, toView(s))
	}
	writeJSON(w, out)
}

// CreateSystem handles POST /v1/systems. Returns the one-time access key.
func (h *Handler) CreateSystem(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name        string          `json:"name"`
		Enabled     *bool           `json:"enabled"`
		AllowUnmask *bool           `json:"allow_unmask"`
		Masking     string          `json:"masking"`
		PII         []detector.Type `json:"pii"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if in.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	key := GenerateAccessKey()
	s := config.SystemConfig{
		Name:        in.Name,
		APIKey:      HashKey(key),
		Enabled:     derefBool(in.Enabled, true),
		AllowUnmask: derefBool(in.AllowUnmask, false),
		Masking:     in.Masking,
		PII:         in.PII,
	}
	if err := h.Repo.CreateSystem(r.Context(), s); err != nil {
		http.Error(w, "create failed", http.StatusConflict)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"name": in.Name, "access_key": key})
}

// GetSystem handles GET /v1/systems/{name}.
func (h *Handler) GetSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	s, err := h.Repo.GetSystem(r.Context(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, toView(*s))
}

// UpdateSystem handles PUT /v1/systems/{name}.
func (h *Handler) UpdateSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var in struct {
		Enabled     *bool           `json:"enabled"`
		AllowUnmask *bool           `json:"allow_unmask"`
		Masking     *string         `json:"masking"`
		PII         []detector.Type `json:"pii"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	cur, err := h.Repo.GetSystem(r.Context(), name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if in.Enabled != nil {
		cur.Enabled = *in.Enabled
	}
	if in.AllowUnmask != nil {
		cur.AllowUnmask = *in.AllowUnmask
	}
	if in.Masking != nil {
		cur.Masking = *in.Masking
	}
	if in.PII != nil {
		cur.PII = in.PII
	}
	if err := h.Repo.UpdateSystem(r.Context(), name, *cur); err != nil {
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]uint64{"rev": h.Mgr.Rev()})
}

// DeleteSystem handles DELETE /v1/systems/{name}.
func (h *Handler) DeleteSystem(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.Repo.DeleteSystem(r.Context(), name); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// RegenerateKey handles POST /v1/systems/{name}/regenerate-key.
func (h *Handler) RegenerateKey(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	key := GenerateAccessKey()
	if err := h.Repo.SetAPIKeyHash(r.Context(), name, HashKey(key)); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err := h.Mgr.Reload(); err != nil {
		http.Error(w, "reload failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"access_key": key})
}

func derefBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}