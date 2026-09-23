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
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/detector"
	"github.com/kind-earthquake/pii-module/internal/queue"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func newTestHandler(t *testing.T, mutate ...func(*config.Config)) *Handler {
	t.Helper()
	cfg := config.Default()
	for _, fn := range mutate {
		fn(cfg)
	}
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(time.Hour, 1000)))
	require.NoError(t, err)
	return &Handler{Mgr: m}
}

func doProcess(t *testing.T, h *Handler, payload, id string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(ProcessRequest{Payload: payload, PayloadID: id})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	return rec
}

func TestProcessThroughPool(t *testing.T) {
	h := newTestHandler(t)
	// Attach a pool to the handler.
	h.Pool = queue.New(4, 64, 16, func(payload string) queue.Result {
		res := h.Mgr.Pipeline("").Process(payload)
		return queue.Result{Masked: res.Masked, Types: res.Types, Tokens: res.Tokens}
	})
	defer h.Pool.Close()
	rec := doProcess(t, h, "паспорт 4509 123456", "pool-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
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

func TestProcessMaskRetryReturnsSameMask(t *testing.T) {
	h := newTestHandler(t)
	// Direct check is retried by the load tester with the SAME payload after a
	// timeout: the endpoint must return the saved mask again, not the original.
	rec := doProcess(t, h, "паспорт 4509 123456", "retry-1")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")

	rec2 := doProcess(t, h, "паспорт 4509 123456", "retry-1")
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp2 ProcessResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	require.Equal(t, resp.Result, resp2.Result)
}

func TestProcessTokenModeMasksAndUnmasks(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) { c.Masking.Mode = "token" })
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
	// Break the store by pointing at a closed miniredis: store failure on the
	// unmask lookup falls back to the mask path.
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	m, err := control.New(
		control.WithConfig(config.Default()),
		control.WithStore(store.NewRedis(client, time.Hour)),
	)
	require.NoError(t, err)
	h := &Handler{Mgr: m}
	mr.Close()
	rec := doProcess(t, h, "паспорт 4509 123456", "id-3")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")
}

func TestProcessSensitiveCooccurrence(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.SensitiveTypes = []detector.Type{detector.TypePIN, detector.TypeCVV}
	})

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
	// Allow only 2 requests, then reject.
	h := newTestHandler(t, func(c *config.Config) {
		c.RateLimit = config.RateLimitConfig{RPS: 2, Burst: 2}
	})

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
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "analytics", Enabled: true, PII: []detector.Type{detector.TypePhone, detector.TypeEmail}, AllowUnmask: false},
		}
	})

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
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "analytics", Enabled: true, AllowUnmask: false},
		}
	})

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
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "down", Enabled: false},
		}
	})
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-3", "down", "")
	require.Equal(t, http.StatusForbidden, rec.Code)
}

func TestProcessSystemUnknown(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true},
		}
	})
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-4", "nope", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestProcessSystemAPIKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), AllowUnmask: true},
		}
	})

	// Без ключа -> метод доступен и обрабатывается (200).
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")

	// Неверный ключ -> 401.
	rec2 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Верный ключ -> 200 (маскирование).
	rec3 := doProcessSystem(t, h, "паспорт 4509 123456", "sys-5", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec3.Code)
	var resp3 ProcessResponse
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	require.Contains(t, resp3.Result, "[ПАСПОРТ]")

	// Внутри системы с allow_unmask=true демаскируется по тому же ключу:
	// повторный запрос с МАСКОЙ (результат маскирования) возвращает оригинал.
	rec4 := doProcessSystem(t, h, resp3.Result, "sys-5", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec4.Code)
	var resp4 ProcessResponse
	require.NoError(t, json.Unmarshal(rec4.Body.Bytes(), &resp4))
	require.Equal(t, "паспорт 4509 123456", resp4.Result)
}

func doProcessSystemBody(t *testing.T, h *Handler, payload, id, system, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(ProcessRequest{Payload: payload, PayloadID: id, System: system, AccessToken: accessToken})
	req := httptest.NewRequest("POST", "/process", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Process(rec, req)
	return rec
}

func TestProcessSystemAccessTokenInBody(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "chat", Enabled: true, APIKey: HashKey("sekret"), AllowUnmask: true},
		}
	})

	// Без токена -> метод доступен и обрабатывается (200).
	rec := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp ProcessResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp.Result, "[ПАСПОРТ]")

	// Неверный токен -> 401.
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "wrong")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)

	// Верный токен в теле -> 200.
	rec3 := doProcessSystemBody(t, h, "паспорт 4509 123456", "tok-1", "chat", "sekret")
	require.Equal(t, http.StatusOK, rec3.Code)
	var resp3 ProcessResponse
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &resp3))
	require.Contains(t, resp3.Result, "[ПАСПОРТ]")
}

func TestProcessSystemRequireKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "locked", Enabled: true, RequireKey: true, AllowUnmask: true},
		}
	})

	// require_key=true, но ключ не задан -> все запросы 401.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-1", "locked", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "rk-1", "locked", "sekret")
	require.Equal(t, http.StatusUnauthorized, rec2.Code)
}

func TestProcessSystemRequireKeyWithKey(t *testing.T) {
	h := newTestHandler(t, func(c *config.Config) {
		c.Systems = []config.SystemConfig{
			{Name: "locked", Enabled: true, APIKey: HashKey("sekret"), RequireKey: true, AllowUnmask: true},
		}
	})

	// require_key=true + ключ задан: без ключа 401, с верным ключом 200.
	rec := doProcessSystem(t, h, "паспорт 4509 123456", "rk-2", "locked", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	rec2 := doProcessSystemBody(t, h, "паспорт 4509 123456", "rk-2", "locked", "sekret")
	require.Equal(t, http.StatusOK, rec2.Code)
}
