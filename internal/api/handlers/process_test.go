package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cfg := &config.Config{AllowUnmask: true, StoreTTL: time.Hour}
	return &Handler{
		Detector: detector.New(detector.StructuredRules()),
		Store:    store.New(client, cfg.StoreTTL),
		Cfg:      cfg,
	}
}

func doProcess(t *testing.T, h *Handler, payload, id string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(ProcessRequest{Payload: payload, PayloadID: id})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	return rec
}

func TestProcessMaskThenUnmask(t *testing.T) {
	h := newTestHandler(t)
	rec := doProcess(t, h, "паспорт 4509 123456", "id-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")

	rec2 := doProcess(t, h, resp.Result, "id-1")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Equal(t, "паспорт 4509 123456", resp2.Result)
}

func TestProcessMalformed(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest("POST", "/process", bytes.NewReader([]byte(`{"payload":"x"}`)))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestProcessNoPII(t *testing.T) {
	h := newTestHandler(t)
	rec := doProcess(t, h, "обычный текст", "id-2")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "обычный текст", resp.Result)
}

func TestProcessUnmaskUnknownID(t *testing.T) {
	h := newTestHandler(t)
	rec := doProcess(t, h, "какая-то маска", "missing-id")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "какая-то маска", resp.Result)
}

func TestProcessRedisDown(t *testing.T) {
	h := newTestHandler(t)
	// Break the store by pointing at a closed miniredis.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h.Store = store.New(client, time.Hour)
	mr.Close()
	rec := doProcess(t, h, "паспорт 4509 123456", "id-3")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
}

func TestProcessSensitiveCooccurrence(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.SensitiveTypes = []detector.Type{detector.TypePIN, detector.TypeCVV}

	// Lone PIN is not masked (co-occurrence rule).
	rec := doProcess(t, h, "пин 1234", "co-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "пин 1234", resp.Result)

	// PIN + card number: both masked.
	rec2 := doProcess(t, h, "пин 1234, карта 4276 1234 5678 9012", "co-2")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Contains(t, resp2.Result, "[ПИН]")
	require.Contains(t, resp2.Result, "[КАРТА]")
}

func TestGateSpans(t *testing.T) {
	h := &Handler{}
	// High confidence → kept.
	spans := []detector.Span{
		{Start: 0, End: 5, Type: detector.TypeEmail, Confidence: 0.99},
	}
	got := h.gateSpans("test@example.com", spans)
	require.Len(t, got, 1)

	// Mid confidence with context → kept.
	spans = []detector.Span{
		{Start: 7, End: 30, Type: detector.TypeFIO, Confidence: 0.8},
	}
	got = h.gateSpans("Клиент Иванов Иван Иванович", spans)
	require.Len(t, got, 1)

	// Mid confidence without context → dropped.
	spans = []detector.Span{
		{Start: 0, End: 20, Type: detector.TypeFIO, Confidence: 0.8},
	}
	got = h.gateSpans("Александр Пушкин написал", spans)
	require.Empty(t, got)

	// Low confidence → dropped.
	spans = []detector.Span{
		{Start: 0, End: 20, Type: detector.TypeAddress, Confidence: 0.5},
	}
	got = h.gateSpans("Банк находится по адресу Москва", spans)
	require.Empty(t, got)
}

func TestProcessRateLimit(t *testing.T) {
	h := newTestHandler(t)
	// Allow only 2 requests, then reject.
	h.Limiter = ratelimit.New(2, 2)

	rec1 := doProcess(t, h, "паспорт 4509 123456", "rl-1")
	require.Equal(t, http.StatusOK, rec1.Code)
	rec2 := doProcess(t, h, "паспорт 4509 123456", "rl-2")
	require.Equal(t, http.StatusOK, rec2.Code)
	// Third request exceeds the limit -> 429 with Retry-After.
	rec3 := doProcess(t, h, "паспорт 4509 123456", "rl-3")
	require.Equal(t, http.StatusTooManyRequests, rec3.Code)
	require.NotEmpty(t, rec3.Header().Get("Retry-After"))
}