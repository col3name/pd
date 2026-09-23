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
	"github.com/kind-earthquake/pii-module/internal/context"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/pipeline"
	"github.com/kind-earthquake/pii-module/internal/ratelimit"
	"github.com/kind-earthquake/pii-module/internal/resolve"
	"github.com/kind-earthquake/pii-module/internal/store"
	"github.com/kind-earthquake/pii-module/internal/whitelist"
)

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	p := pipeline.New(
		detector.New(detector.StructuredRules()),
		context.New(context.DefaultBoost, context.DefaultPenalty),
		whitelist.New(cfg.Whitelist.Persons, cfg.Whitelist.Addresses, cfg.Whitelist.Organizations),
		resolve.DefaultPriority,
		pipeline.Options{Mode: cfg.Masking.Mode, Sensitive: cfg.SensitiveTypes},
	)
	return &Handler{
		Pipeline: p,
		Store:    store.NewMemory(time.Hour, 1000),
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

func TestProcessTokenModeMasksAndUnmasks(t *testing.T) {
	h := newTestHandler(t)
	// The pipeline captures the masking mode at construction, so rebuild it in
	// token mode against the same store/config.
	h.Cfg.Masking.Mode = "token"
	h.Pipeline = pipeline.New(
		detector.New(detector.StructuredRules()),
		context.New(context.DefaultBoost, context.DefaultPenalty),
		whitelist.New(h.Cfg.Whitelist.Persons, h.Cfg.Whitelist.Addresses, h.Cfg.Whitelist.Organizations),
		resolve.DefaultPriority,
		pipeline.Options{Mode: h.Cfg.Masking.Mode, Sensitive: h.Cfg.SensitiveTypes},
	)
	rec := doProcess(t, h, "Клиент Иванов Иван Иванович, паспорт 4509 123456", "tok-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[PERSON_001]")

	rec2 := doProcess(t, h, resp.Result, "tok-1")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Equal(t, "Клиент Иванов Иван Иванович, паспорт 4509 123456", resp2.Result)
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
	// Break the store by pointing at a closed miniredis: store failure on the
	// unmask lookup falls back to the mask path.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	h.Store = store.NewRedis(client, time.Hour)
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

func doProcessSystem(t *testing.T, h *Handler, payload, id, system, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(ProcessRequest{Payload: payload, PayloadID: id, System: system})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	return rec
}

func TestProcessSystemAllowlistTypes(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.Systems = []config.SystemConfig{
		{Name: "analytics", Enabled: true, PII: []detector.Type{detector.TypePhone, detector.TypeEmail}, AllowUnmask: false},
	}

	rec := doProcessSystem(t, h, "Клиент Иванов Иван, тел +7 912 345-67-89, email ivanov@test.ru, паспорт 4509 123456", "sys-1", "analytics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// Только разрешённые типы замаскированы: телефон и email, но не ФИО и паспорт.
	require.Contains(t, resp.Result, "[ТЕЛЕФОН]")
	require.Contains(t, resp.Result, "[EMAIL]")
	require.Contains(t, resp.Result, "Иванов Иван")
	require.Contains(t, resp.Result, "4509 123456")
}

func TestProcessSystemNoUnmask(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.Systems = []config.SystemConfig{
		{Name: "analytics", Enabled: true, AllowUnmask: false},
	}

	// Маскируем.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-2", "analytics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")

	// Повторный запрос с тем же id НЕ демаскируется (allow_unmask=false).
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-2", "analytics", "")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Contains(t, resp2.Result, "[ПАСПОРТ]")
}

func TestProcessSystemDisabled(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.Systems = []config.SystemConfig{
		{Name: "down", Enabled: false},
	}
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-3", "down", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestProcessSystemUnknown(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.Systems = []config.SystemConfig{
		{Name: "chat", Enabled: true},
	}
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-4", "nope", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProcessSystemAPIKey(t *testing.T) {
	h := newTestHandler(t)
	h.Cfg.Systems = []config.SystemConfig{
		{Name: "chat", Enabled: true, APIKey: "sekret", AllowUnmask: true},
	}

	// Без ключа -> 401.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Неверный ключ -> 401.
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Верный ключ -> 200.
	rec3 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec3.Code)

	// Внутри системы с allow_unmask=true демаскируется по тому же ключу.
	rec4 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec4.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &resp))
	require.Equal(t, "паспорт 4509 123456", resp.Result)
}