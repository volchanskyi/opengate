package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthMiddlewareRejectsTokenWithoutTenant covers the token a caller still
// holds from before the tenancy rename: correctly signed and unexpired, but
// naming its tenant under a key this build does not read. The middleware must
// answer 401 — the response the web client turns into a fresh login — rather
// than admitting a request that would then fail without tenant scope deep
// inside a handler.
func TestAuthMiddlewareRejectsTokenWithoutTenant(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	now := time.Now()

	stale := jwt.MapClaims{
		"uid":   h.userID.String(),
		"email": testEmailUser,
		"admin": false,
		"iss":   h.cfg.Issuer,
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, stale).SignedString([]byte(h.cfg.Secret))
	require.NoError(t, err)

	reached := false
	w := serveAuthMiddleware(h, "Bearer "+signed, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid token")
	assert.False(t, reached, "a token with no tenant must never reach a handler")
}
