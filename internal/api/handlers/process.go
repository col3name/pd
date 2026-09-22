package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/masker"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/store"
)

// ProcessRequest is the /process request body.
type ProcessRequest struct {
	Payload   string `json:"payload"`
	PayloadID string `json:"payload_id"`
}

// ProcessResponse is the /process response body.
type ProcessResponse struct {
	Result string `json:"result"`
}

// Handler serves POST /process.
type Handler struct {
	Detector *detector.Detector
	Store    *store.Store
	Cfg      *config.Config
}

// Process handles masking (new payload_id) and unmasking (existing payload_id).
func (h *Handler) Process(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var req ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PayloadID == "" {
		http.Error(w, "payload_id is required", http.StatusBadRequest)
		return
	}

	// Unmask path: existing payload_id.
	if h.Cfg.AllowUnmask {
		if original, ok, err := h.Store.Get(r.Context(), req.PayloadID); err == nil && ok {
			writeResult(w, ProcessResponse{Result: original})
			observability.RequestsTotal.WithLabelValues("unmask", "200").Inc()
			observability.RequestLatency.WithLabelValues("unmask").Observe(time.Since(start).Seconds())
			return
		}
	}

	// Mask path: detect, mask, save original.
	spans := h.Detector.Detect(req.Payload)
	masked := masker.Mask(req.Payload, spans)
	for _, s := range spans {
		observability.DetectedTotal.WithLabelValues(string(s.Type)).Inc()
	}
	if err := h.Store.Save(r.Context(), req.PayloadID, req.Payload); err != nil {
		// Degrade gracefully: masking still works, unmask will fail-open.
		slog.Warn("store save failed", "payload_id", req.PayloadID, "error", err)
	}
	writeResult(w, ProcessResponse{Result: masked})
	observability.RequestsTotal.WithLabelValues("mask", "200").Inc()
	observability.RequestLatency.WithLabelValues("mask").Observe(time.Since(start).Seconds())
}

func writeResult(w http.ResponseWriter, resp ProcessResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}