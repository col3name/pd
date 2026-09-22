package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/masker"
	"github.com/kind-earthquake/pii-module/internal/observability"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
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
	Limiter  *ratelimit.Limiter
}

// Process handles masking (new payload_id) and unmasking (existing payload_id).
func (h *Handler) Process(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// Rate limit: return 429 with Retry-After when the bucket is empty.
	if h.Limiter != nil {
		if ok, retryAfter := h.Limiter.Allow(); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			observability.RequestsTotal.WithLabelValues("mask", "429").Inc()
			slog.Warn("process: rate limited", "retry_after_s", retryAfter.Seconds())
			return
		}
	}

	var req ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("process: invalid request body", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.PayloadID == "" {
		slog.Warn("process: missing payload_id")
		http.Error(w, "payload_id is required", http.StatusBadRequest)
		return
	}

	// Unmask path: existing payload_id.
	if h.Cfg.AllowUnmask {
		if original, ok, err := h.Store.Get(r.Context(), req.PayloadID); err == nil && ok {
			writeResult(w, ProcessResponse{Result: original})
			observability.RequestsTotal.WithLabelValues("unmask", "200").Inc()
			observability.RequestLatency.WithLabelValues("unmask").Observe(time.Since(start).Seconds())
			slog.Info("process: unmasked", "payload_id", req.PayloadID, "latency_ms", time.Since(start).Milliseconds())
			return
		}
	}

	// Mask path: detect, mask, save original.
	spans := h.gateSpans(req.Payload, h.Detector.Detect(req.Payload))
	// Co-occurrence rule: a lone sensitive type (e.g. PIN without a card
	// number) is not masked.
	if len(spans) == 1 && h.isSensitive(spans[0].Type) {
		spans = nil
	}
	masked := masker.Mask(req.Payload, spans)
	types := make([]string, 0, len(spans))
	for _, s := range spans {
		types = append(types, string(s.Type))
		observability.DetectedTotal.WithLabelValues(string(s.Type)).Inc()
	}
	if err := h.Store.Save(r.Context(), req.PayloadID, req.Payload); err != nil {
		// Degrade gracefully: masking still works, unmask will fail-open.
		slog.Warn("process: store save failed", "payload_id", req.PayloadID, "error", err)
	}
	writeResult(w, ProcessResponse{Result: masked})
	observability.RequestsTotal.WithLabelValues("mask", "200").Inc()
	observability.RequestLatency.WithLabelValues("mask").Observe(time.Since(start).Seconds())
	slog.Info("process: masked", "payload_id", req.PayloadID, "types", types, "latency_ms", time.Since(start).Milliseconds())
}

func writeResult(w http.ResponseWriter, resp ProcessResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// isSensitive reports whether t is a sensitive type that requires co-occurrence
// with another PII type to be masked.
func (h *Handler) isSensitive(t detector.Type) bool {
	for _, s := range h.Cfg.SensitiveTypes {
		if s == t {
			return true
		}
	}
	return false
}

// gateSpans filters spans by the 3-threshold confidence model.
func (h *Handler) gateSpans(text string, spans []detector.Span) []detector.Span {
	var kept []detector.Span
	for _, s := range spans {
		switch {
		case s.Confidence >= 0.95:
			kept = append(kept, s)
		case s.Confidence >= 0.75:
			if detector.HasContext(text, s.Start, s.End, s.Type) {
				kept = append(kept, s)
			}
		default:
			// below 0.75 → drop
		}
	}
	return kept
}