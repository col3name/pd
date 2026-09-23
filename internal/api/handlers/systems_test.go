package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/kind-earthquake/pii-module/internal/detector"
)

// systemsRouter builds a chi router with the systems routes wired to h.
func systemsRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/systems", h.ListSystems)
	r.Post("/systems", h.CreateSystem)
	r.Get("/systems/{name}", h.GetSystem)
	r.Put("/systems/{name}", h.UpdateSystem)
	r.Delete("/systems/{name}", h.DeleteSystem)
	r.Post("/systems/{name}/regenerate-key", h.RegenerateKey)
	return r
}

func TestSystemsCRUD(t *testing.T) {
	h := newDBTestHandler(t)
	router := systemsRouter(h)

	// Create.
	body, _ := json.Marshal(map[string]any{
		"name":         "chat",
		"enabled":      true,
		"allow_unmask": true,
		"masking":      "token",
		"pii":          []detector.Type{detector.TypePhone, detector.TypeEmail},
	})
	req := httptest.NewRequest("POST", "/systems", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var created struct {
		Name      string `json:"name"`
		AccessKey string `json:"access_key"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	require.Equal(t, "chat", created.Name)
	require.NotEmpty(t, created.AccessKey)

	// Get.
	req2 := httptest.NewRequest("GET", "/systems/chat", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var view systemView
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &view))
	require.Equal(t, "chat", view.Name)
	require.True(t, view.APIKeySet)
	require.True(t, view.Enabled)
	require.True(t, view.AllowUnmask)
	require.Equal(t, "token", view.Masking)

	// List.
	req3 := httptest.NewRequest("GET", "/systems", nil)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
	var list []systemView
	require.NoError(t, json.Unmarshal(rec3.Body.Bytes(), &list))
	require.Len(t, list, 1)
	require.Equal(t, "chat", list[0].Name)

	// Update.
	upd, _ := json.Marshal(map[string]any{"enabled": false})
	req4 := httptest.NewRequest("PUT", "/systems/chat", bytes.NewReader(upd))
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, req4)
	require.Equal(t, http.StatusOK, rec4.Code)

	// Verify update persisted.
	req5 := httptest.NewRequest("GET", "/systems/chat", nil)
	rec5 := httptest.NewRecorder()
	router.ServeHTTP(rec5, req5)
	require.Equal(t, http.StatusOK, rec5.Code)
	var view2 systemView
	require.NoError(t, json.Unmarshal(rec5.Body.Bytes(), &view2))
	require.False(t, view2.Enabled)

	// Regenerate key.
	req6 := httptest.NewRequest("POST", "/systems/chat/regenerate-key", nil)
	rec6 := httptest.NewRecorder()
	router.ServeHTTP(rec6, req6)
	require.Equal(t, http.StatusOK, rec6.Code)
	var regen struct {
		AccessKey string `json:"access_key"`
	}
	require.NoError(t, json.Unmarshal(rec6.Body.Bytes(), &regen))
	require.NotEmpty(t, regen.AccessKey)

	// Delete.
	req7 := httptest.NewRequest("DELETE", "/systems/chat", nil)
	rec7 := httptest.NewRecorder()
	router.ServeHTTP(rec7, req7)
	require.Equal(t, http.StatusNoContent, rec7.Code)

	// Get after delete -> 404.
	req8 := httptest.NewRequest("GET", "/systems/chat", nil)
	rec8 := httptest.NewRecorder()
	router.ServeHTTP(rec8, req8)
	require.Equal(t, http.StatusNotFound, rec8.Code)
}

func TestCreateSystemMissingName(t *testing.T) {
	h := newDBTestHandler(t)
	body, _ := json.Marshal(map[string]any{"enabled": true})
	req := httptest.NewRequest("POST", "/v1/systems", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateSystem(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateSystemDuplicate(t *testing.T) {
	h := newDBTestHandler(t)
	body, _ := json.Marshal(map[string]any{"name": "dup"})
	req := httptest.NewRequest("POST", "/v1/systems", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateSystem(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	req2 := httptest.NewRequest("POST", "/v1/systems", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	h.CreateSystem(rec2, req2)
	require.Equal(t, http.StatusConflict, rec2.Code)
}