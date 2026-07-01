package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestCreateEnrollmentToken(t *testing.T) {
	t.Parallel()
	t.Run("admin success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		label := "test-token"
		maxUses := 5
		expiresInHours := 48
		body := CreateEnrollmentTokenRequest{
			Label:          &label,
			MaxUses:        &maxUses,
			ExpiresInHours: &expiresInHours,
		}

		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))
		assert.Equal(t, "test-token", tok.Label)
		assert.Equal(t, 5, tok.MaxUses)
		assert.Equal(t, 0, tok.UseCount)
		assert.NotEmpty(t, tok.Token)
		assert.Len(t, tok.Token, 64) // 32 bytes hex
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", userToken, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestListEnrollmentTokens(t *testing.T) {
	t.Parallel()
	t.Run("returns created tokens", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Create two tokens.
		for _, label := range []string{"token-1", "token-2"} {
			l := label
			body := CreateEnrollmentTokenRequest{Label: &l}
			w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
			require.Equal(t, http.StatusCreated, w.Code)
		}

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Len(t, tokens, 2)
	})

	t.Run("empty list", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Empty(t, tokens)
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", userToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
