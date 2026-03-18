package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testPathRegister = "/api/v1/auth/register"
	testPathLogin    = "/api/v1/auth/login"
	testLoginEmail   = "login@example.com"
)

func TestRegisterHandler(t *testing.T) {
	srv, _ := newTestServer(t)

	t.Run("successful registration", func(t *testing.T) {
		body := map[string]string{
			"email":        "new@example.com",
			"password":     "secret123",
			"display_name": "New User",
		}
		w := doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp TokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("missing email", func(t *testing.T) {
		body := map[string]string{"password": "secret"}
		w := doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing password", func(t *testing.T) {
		body := map[string]string{"email": "x@example.com"}
		w := doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicate email returns generic error", func(t *testing.T) {
		email := "dup@example.com"
		body := map[string]string{"email": email, "password": "password123"}
		w := doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusCreated, w.Code)

		// Second registration with same email.
		w = doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		// Error message must not reveal the email exists.
		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Equal(t, "registration failed", errResp["error"])
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, testPathRegister, "", "not-json{{{")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestLoginHandler(t *testing.T) {
	srv, cfg := newTestServer(t)
	seedTestUser(t, srv, cfg, testLoginEmail, false)

	t.Run("successful login", func(t *testing.T) {
		body := map[string]string{"email": testLoginEmail, "password": "password123"}
		w := doRequest(srv, http.MethodPost, testPathLogin, "", body)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp TokenResponse
		json.NewDecoder(w.Body).Decode(&resp)
		assert.NotEmpty(t, resp.Token)
	})

	t.Run("wrong password", func(t *testing.T) {
		body := map[string]string{"email": testLoginEmail, "password": "wrong"}
		w := doRequest(srv, http.MethodPost, testPathLogin, "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("unknown email", func(t *testing.T) {
		body := map[string]string{"email": "nobody@example.com", "password": "pass"}
		w := doRequest(srv, http.MethodPost, testPathLogin, "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid json body", func(t *testing.T) {
		w := doRawRequest(srv, http.MethodPost, testPathLogin, "", "bad json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing fields", func(t *testing.T) {
		body := map[string]string{"email": testLoginEmail}
		w := doRequest(srv, http.MethodPost, testPathLogin, "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
