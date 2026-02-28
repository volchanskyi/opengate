package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterHandler(t *testing.T) {
	srv, _ := newTestServer(t)

	t.Run("successful registration", func(t *testing.T) {
		body := map[string]string{
			"email":        "new@example.com",
			"password":     "secret123",
			"display_name": "New User",
		}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp tokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("missing email", func(t *testing.T) {
		body := map[string]string{"password": "secret"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing password", func(t *testing.T) {
		body := map[string]string{"email": "x@example.com"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/register", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, "/api/v1/auth/register", "", "not-json{{{")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestLoginHandler(t *testing.T) {
	srv, cfg := newTestServer(t)
	seedTestUser(t, srv, cfg, "login@example.com", false)

	t.Run("successful login", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com", "password": "password123"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp tokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("wrong password", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com", "password": "wrong"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unknown email", func(t *testing.T) {
		body := map[string]string{"email": "nobody@example.com", "password": "pass"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, "/api/v1/auth/login", "", "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing fields", func(t *testing.T) {
		body := map[string]string{"email": "login@example.com"}
		w := doRequest(srv, http.MethodPost, "/api/v1/auth/login", "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
