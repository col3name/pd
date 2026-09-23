package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/config"
	"github.com/kind-earthquake/pii-module/internal/control"
	"github.com/kind-earthquake/pii-module/internal/store"
)

func newConfigHandler(t *testing.T) *Handler {
	t.Helper()
	cfg := config.Default()
	m, err := control.New(control.WithConfig(cfg), control.WithStore(store.NewMemory(0, 1000)))
	require.NoError(t, err)
	h := &Handler{Mgr: m}
	return h
}

func TestGetConfigView(t *testing.T) {
	h := newConfigHandler(t)
	rec := httptest.NewRecorder()
	h.GetConfig(rec, httptest.NewRequest(http.MethodGet, "/v1/config", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var view map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.Equal(t, float64(0), view["rev"])
	require.Contains(t, view, "systems")
	require.Contains(t, view, "known_types")
}

func TestGetConfigRules(t *testing.T) {
	h := newConfigHandler(t)
	rec := httptest.NewRecorder()
	h.GetConfigRules(rec, httptest.NewRequest(http.MethodGet, "/v1/config/rules", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.NotEmpty(t, body["types"])
}
