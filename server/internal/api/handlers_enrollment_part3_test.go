package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestDeleteEnrollmentToken(t *testing.T) {
	t.Parallel()
	t.Run("success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		// Create a token.
		body := CreateEnrollmentTokenRequest{}
		w := doRequest(srv, http.MethodPost, "/api/v1/enrollment-tokens", adminToken, body)
		require.Equal(t, http.StatusCreated, w.Code)

		var tok EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tok))

		// Delete it.
		w = doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/"+tok.Id.String(), adminToken, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify it's gone.
		w = doRequest(srv, http.MethodGet, "/api/v1/enrollment-tokens", adminToken, nil)
		require.Equal(t, http.StatusOK, w.Code)

		var tokens []EnrollmentToken
		require.NoError(t, json.NewDecoder(w.Body).Decode(&tokens))
		assert.Empty(t, tokens)
	})

	t.Run("not found", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

		w := doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/00000000-0000-0000-0000-000000000000", adminToken, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("non-admin forbidden", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodDelete, "/api/v1/enrollment-tokens/00000000-0000-0000-0000-000000000000", userToken, nil)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
