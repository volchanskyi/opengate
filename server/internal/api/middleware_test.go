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
)

const (
	testEmailUser = "user@example.com"
)

func TestAuthMiddleware(t *testing.T) {
	cfg := testJWTConfig()
	userID := uuid.New()

	// handler that records whether it was reached
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := ContextClaims(r.Context())
		if claims != nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(claims.Email))
		}
	})

	middleware := AuthMiddleware(cfg)

	t.Run("valid token passes through", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, testEmailUser, false)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		middleware(okHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, testEmailUser, w.Body.String())
	})

	t.Run("missing header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		w := httptest.NewRecorder()

		middleware(okHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid format returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Token abc123")
		w := httptest.NewRecorder()

		middleware(okHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("expired token returns 401", func(t *testing.T) {
		expCfg := &auth.JWTConfig{
			Secret:   cfg.Secret,
			Issuer:   cfg.Issuer,
			Duration: -1 * time.Hour,
		}
		token, err := expCfg.GenerateToken(userID, testEmailUser, false)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()

		middleware(okHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("bearer case insensitive", func(t *testing.T) {
		token, err := cfg.GenerateToken(userID, testEmailUser, false)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "bearer "+token)
		w := httptest.NewRecorder()

		middleware(okHandler).ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestContextHelpers(t *testing.T) {
	t.Run("ContextClaims returns nil for empty context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Nil(t, ContextClaims(req.Context()))
	})

	t.Run("ContextUserID returns Nil for empty context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Equal(t, uuid.Nil, ContextUserID(req.Context()))
	})
}
