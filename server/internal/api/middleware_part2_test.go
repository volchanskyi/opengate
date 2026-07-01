package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

type authMiddlewareHarness struct {
	cfg        *auth.JWTConfig
	middleware func(http.Handler) http.Handler
	okHandler  http.Handler
	userID     uuid.UUID
}

func newAuthMiddlewareHarness() authMiddlewareHarness {
	cfg := testJWTConfig()
	userID := uuid.New()
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if claims := ContextClaims(r.Context()); claims != nil {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(claims.Email))
		}
	})
	return authMiddlewareHarness{cfg: cfg, middleware: AuthMiddleware(cfg), okHandler: okHandler, userID: userID}
}

func TestAuthMiddlewareValidTokenPassesThrough(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	token, err := h.cfg.GenerateToken(h.userID, testEmailUser, false)
	require.NoError(t, err)

	w := serveAuthMiddleware(h, "Bearer "+token, h.okHandler)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, testEmailUser, w.Body.String())
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	w := serveAuthMiddleware(h, "", h.okHandler)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddlewareInvalidFormat(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	w := serveAuthMiddleware(h, "Token abc123", h.okHandler)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddlewareExpiredToken(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	expCfg := &auth.JWTConfig{Secret: h.cfg.Secret, Issuer: h.cfg.Issuer, Duration: -1 * time.Hour}
	token, err := expCfg.GenerateToken(h.userID, testEmailUser, false)
	require.NoError(t, err)

	w := serveAuthMiddleware(h, "Bearer "+token, h.okHandler)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddlewareBearerCaseInsensitive(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	token, err := h.cfg.GenerateToken(h.userID, testEmailUser, false)
	require.NoError(t, err)

	w := serveAuthMiddleware(h, "bearer "+token, h.okHandler)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddlewareInjectsTenantScope(t *testing.T) {
	t.Parallel()
	h := newAuthMiddlewareHarness()
	orgID := uuid.New()
	token, err := h.cfg.GenerateToken(h.userID, testEmailUser, true, orgID)
	require.NoError(t, err)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tenant, ok := dbtx.TenantFromContext(r.Context())
		require.True(t, ok)
		assert.Equal(t, orgID, tenant.OrgID)
		assert.True(t, tenant.IsAdmin)
		w.WriteHeader(http.StatusOK)
	})
	w := serveAuthMiddleware(h, "Bearer "+token, handler)
	assert.Equal(t, http.StatusOK, w.Code)
}

func serveAuthMiddleware(h authMiddlewareHarness, authorization string, handler http.Handler) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	w := httptest.NewRecorder()
	h.middleware(handler).ServeHTTP(w, req)
	return w
}
