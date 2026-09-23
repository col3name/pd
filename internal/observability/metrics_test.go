package observability

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetricsHandler(t *testing.T) {
	RequestsTotal.WithLabelValues("mask", "200").Inc()
	DetectedTotal.WithLabelValues("ПАСПОРТ").Inc()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "pii_requests_total")
	require.Contains(t, body, "pii_detected_total")
}

func TestNewGaugesRegistered(t *testing.T) {
	QueueDepth.Set(5)
	WorkersBusy.Set(3)
	RedisUp.Set(1)
	require.Equal(t, 5.0, testutil.ToFloat64(QueueDepth))
	require.Equal(t, 3.0, testutil.ToFloat64(WorkersBusy))
	require.Equal(t, 1.0, testutil.ToFloat64(RedisUp))
	require.Equal(t, 1, testutil.CollectAndCount(QueueDepth))
	require.Equal(t, 1, testutil.CollectAndCount(WorkersBusy))
	require.Equal(t, 1, testutil.CollectAndCount(RedisUp))
}