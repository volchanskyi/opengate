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

type authCase struct {
	name string
	body map[string]string
	raw  string
	want int
}

func runAuthCases(t *testing.T, srv *Server, path string, cases []authCase) {
	t.Helper()
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var w = doRequest(srv, http.MethodPost, path, "", tt.body)
			if tt.raw != "" {
				w = doRawRequest(srv, http.MethodPost, path, "", tt.raw)
			}
			assert.Equal(t, tt.want, w.Code)
			if tt.want < 300 {
				var resp TokenResponse
				json.NewDecoder(w.Body).Decode(&resp)
				assert.NotEmpty(t, resp.Token)
			}
		})
	}
}

func TestRegisterHandler(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	runAuthCases(t, srv, testPathRegister, []authCase{
		{"successful registration", map[string]string{
			"email": "new@example.com", "password": "secret123", "display_name": "New User",
		}, "", http.StatusCreated},
		{"missing email", map[string]string{"password": "secret"}, "", http.StatusBadRequest},
		{"missing password", map[string]string{"email": "x@example.com"}, "", http.StatusBadRequest},
		{"invalid json body", nil, "not-json{{{", http.StatusBadRequest},
	})

	t.Run("duplicate email returns generic error", func(t *testing.T) {
		email := "dup@example.com"
		body := map[string]string{"email": email, "password": "password123"}
		w := doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusCreated, w.Code)

		w = doRequest(srv, http.MethodPost, testPathRegister, "", body)
		assert.Equal(t, http.StatusBadRequest, w.Code)

		var errResp map[string]string
		json.NewDecoder(w.Body).Decode(&errResp)
		assert.Equal(t, "registration failed", errResp["error"])
	})

	emailCases := func(emails map[string]string, want int) []authCase {
		var cases []authCase
		for name, email := range emails {
			cases = append(cases, authCase{name, map[string]string{"email": email, "password": "password123"}, "", want})
		}
		return cases
	}
	t.Run("invalid email format", func(t *testing.T) {
		runAuthCases(t, srv, testPathRegister, emailCases(map[string]string{
			"no at sign": "invalid-email",
			"no domain":  "user@",
			"spaces":     "user @example.com",
		}, http.StatusBadRequest))
	})
	t.Run("valid email formats accepted", func(t *testing.T) {
		runAuthCases(t, srv, testPathRegister, emailCases(map[string]string{
			"simple":    "valid@example.com",
			"subdomain": "user@sub.example.com",
			"plus tag":  "user+tag@example.com",
		}, http.StatusCreated))
	})
}

func TestLoginHandler(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	seedTestUser(t, srv, cfg, testLoginEmail, false)

	runAuthCases(t, srv, testPathLogin, []authCase{
		{"successful login", map[string]string{"email": testLoginEmail, "password": "password123"}, "", http.StatusOK},
		{"wrong password", map[string]string{"email": testLoginEmail, "password": "wrong"}, "", http.StatusUnauthorized},
		{"unknown email", map[string]string{"email": "nobody@example.com", "password": "pass"}, "", http.StatusUnauthorized},
		{"invalid json body", nil, "bad json", http.StatusBadRequest},
		{"missing fields", map[string]string{"email": testLoginEmail}, "", http.StatusBadRequest},
	})
}
