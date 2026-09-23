package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoginLogout(t *testing.T) {
	h := newDBTestHandler(t)
	// Seed an admin directly on the repo.
	require.NoError(t, h.Repo.SeedAdmin(t.Context(), "admin", "secret"))

	// Login with wrong password -> 401.
	body, _ := json.Marshal(map[string]string{"login": "admin", "password": "nope"})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	// Login with correct password -> 200 + token.
	body2, _ := json.Marshal(map[string]string{"login": "admin", "password": "secret"})
	req2 := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body2))
	rec2 := httptest.NewRecorder()
	h.Login(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Token)

	// Logout with the token -> 200.
	req3 := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req3.Header.Set("Authorization", "Bearer "+resp.Token)
	rec3 := httptest.NewRecorder()
	h.Logout(rec3, req3)
	require.Equal(t, http.StatusOK, rec3.Code)
}