package api

import (
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestGetServerCA(t *testing.T) {
	t.Parallel()
	t.Run("authenticated success", func(t *testing.T) {
		srv, cfg := newTestServerWithCert(t)
		_, userToken := seedTestUser(t, srv, cfg, "user@test.com", false)

		w := doRequest(srv, http.MethodGet, "/api/v1/server/ca", userToken, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp CACertResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp.Pem, "BEGIN CERTIFICATE")
	})

	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServerWithCert(t)

		w := doRequest(srv, http.MethodGet, "/api/v1/server/ca", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}
