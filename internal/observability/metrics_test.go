package observability

import (
	"net/http/httptest"
	"testing"

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