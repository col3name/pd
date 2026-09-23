package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/db"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/pipeline"
	"github.com/kind-earthquake/pii-module/internal/store"
)

type ProcessRequest struct {
	Payload     string `json:"payload"`
	PayloadID   string `json:"payload_id"`
	System      string `json:"system"`
	AccessToken string `json:"access_token"`
}

type ProcessResponse struct {
	Result string `json:"result"`
}

type Handler struct {
	Mgr  *control.Manager
	Repo *db.Repo
}

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

// Process handles masking (new payload_id) and unmasking (existing payload_id).
func (h *Handler) Process(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if lim := h.Mgr.Limiter(); lim != nil {
		if ok, retryAfter := lim.Allow(); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			observability.RequestsTotal.WithLabelValues("mask", "429").Inc()
			return
		}
	}
	var req ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PayloadID == "" {
		http.Error(w, "payload_id is required", http.StatusBadRequest)
		return
	}

	// System authorization: unknown system -> 404, disabled -> 403, bad key -> 401.
	system := h.systemConfig(req.System)
	if req.System != "" && system == nil {
		http.Error(w, "unknown system", http.StatusNotFound)
		return
	}
	if system != nil && !system.Enabled {
		http.Error(w, "system disabled", http.StatusForbidden)
		return
	}
	if err := h.authorize(r, system, req.AccessToken); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	allowUnmask := h.Mgr.Config().AllowUnmask
	if system != nil {
		allowUnmask = system.AllowUnmask
	}
	if allowUnmask {
		// A payload_id is reusable: the direct check may be retried (the load
		// tester re-sends the same payload after a timeout) and the reverse
		// check re-sends our masked result. Distinguish by matching payload:
		//   payload == Original -> direct check retry -> return the saved mask
		//   payload == Masked   -> reverse check     -> return the original
		if e, ok, err := h.Mgr.Store().Get(r.Context(), req.PayloadID); err == nil && ok {
			if e.Masked != "" && req.Payload == e.Original {
				writeResult(w, ProcessResponse{Result: e.Masked})
				observability.RequestsTotal.WithLabelValues("mask", "200").Inc()
				observability.RequestLatency.WithLabelValues("mask").Observe(time.Since(start).Seconds())
				slog.Info("process: mask retry served", "payload_id", req.PayloadID, "system", req.System, "latency_ms", time.Since(start).Milliseconds())
				return
			}
			if req.Payload == e.Masked {
				writeResult(w, ProcessResponse{Result: e.Original})
				observability.RequestsTotal.WithLabelValues("unmask", "200").Inc()
				observability.RequestLatency.WithLabelValues("unmask").Observe(time.Since(start).Seconds())
				slog.Info("process: unmasked", "payload_id", req.PayloadID, "system", req.System, "latency_ms", time.Since(start).Milliseconds())
				return
			}
		}
	}
	p, ok := h.systemPipeline(req.System)
	if !ok {
		http.Error(w, "system disabled", http.StatusForbidden)
		return
	}
	res := p.Process(req.Payload)
	for _, t := range res.Types {
		observability.DetectedTotal.WithLabelValues(t).Inc()
	}
	if err := h.Mgr.Store().Save(r.Context(), req.PayloadID, store.Entry{Original: req.Payload, Masked: res.Masked, Tokens: res.Tokens}); err != nil {
		slog.Warn("process: store save failed", "payload_id", req.PayloadID, "error", err)
	}
	writeResult(w, ProcessResponse{Result: res.Masked})
	observability.RequestsTotal.WithLabelValues("mask", "200").Inc()
	observability.RequestLatency.WithLabelValues("mask").Observe(time.Since(start).Seconds())
	slog.Info("process: masked", "payload_id", req.PayloadID, "system", req.System, "types", res.Types, "latency_ms", time.Since(start).Milliseconds())
}

// systemConfig looks up a consumer system by name (nil if absent).
func (h *Handler) systemConfig(name string) *config.SystemConfig {
	return h.Mgr.System(name)
}

// authorize enforces the per-system API key via the X-API-Key header or the
// access_token body field. Systems without a configured key are authorized
// implicitly, unless require_key is set.
func (h *Handler) authorize(r *http.Request, s *config.SystemConfig, bodyToken string) error {
	if s == nil {
		return nil
	}
	key := r.Header.Get("X-API-Key")
	if key == "" {
		key = bodyToken
	}
	if s.RequireKey {
		if s.APIKey == "" || key == "" || HashKey(key) != s.APIKey {
			return config.ErrUnauthorized
		}
		return nil
	}
	if s.APIKey == "" {
		return nil
	}
	if HashKey(key) == s.APIKey {
		return nil
	}
	return config.ErrUnauthorized
}

func writeResult(w http.ResponseWriter, resp ProcessResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
