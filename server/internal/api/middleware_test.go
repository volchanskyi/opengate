package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
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

func TestSecurityHeaders(t *testing.T) {
	srv, _ := newTestServer(t)

	w := doRequest(srv, http.MethodGet, "/api/v1/health", "", nil)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get("Referrer-Policy"))
}

func TestMaxBodySize(t *testing.T) {
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, "bodysize@example.com", false)

	t.Run("small body accepted", func(t *testing.T) {
		body := map[string]string{"name": "test-group"}
		w := doRequest(srv, http.MethodPost, "/api/v1/groups", token, body)
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("oversized body rejected", func(t *testing.T) {
		// Create a body larger than 1 MB.
		huge := make([]byte, maxRequestBodySize+1)
		for i := range huge {
			huge[i] = 'a'
		}
		w := doRawRequest(srv, http.MethodPost, "/api/v1/groups", token, string(huge))
		assert.True(t, w.Code == http.StatusBadRequest || w.Code == http.StatusRequestEntityTooLarge,
			"expected 400 or 413, got %d", w.Code)
	})
}

func TestIsGroupOwner(t *testing.T) {
	srv, cfg := newTestServer(t)
	owner, ownerToken := seedTestUser(t, srv, cfg, "owner@example.com", false)
	_ = ownerToken

	// Create a group owned by the user.
	groupID := uuid.New()
	err := srv.store.CreateGroup(t.Context(), &db.Group{
		ID:      groupID,
		Name:    "test-group",
		OwnerID: owner.ID,
	})
	require.NoError(t, err)

	// Helper to build a context with claims for a specific user.
	ctxWithUser := func(userID uuid.UUID, admin bool) context.Context {
		claims := &auth.Claims{
			UserID:  userID,
			Email:   "test@test.com",
			IsAdmin: admin,
		}
		return context.WithValue(t.Context(), claimsKey, claims)
	}

	t.Run("admin always returns true", func(t *testing.T) {
		ctx := ctxWithUser(uuid.New(), true)
		assert.True(t, srv.isGroupOwner(ctx, groupID))
		assert.True(t, srv.isGroupOwner(ctx, uuid.Nil))
	})

	t.Run("owner of group returns true", func(t *testing.T) {
		ctx := ctxWithUser(owner.ID, false)
		assert.True(t, srv.isGroupOwner(ctx, groupID))
	})

	t.Run("non-owner of group returns false", func(t *testing.T) {
		ctx := ctxWithUser(uuid.New(), false)
		assert.False(t, srv.isGroupOwner(ctx, groupID))
	})

	t.Run("nil group ID returns true for any authenticated user", func(t *testing.T) {
		ctx := ctxWithUser(uuid.New(), false)
		assert.True(t, srv.isGroupOwner(ctx, uuid.Nil), "ungrouped devices should be accessible to all authenticated users")
	})

	t.Run("non-existent group returns false", func(t *testing.T) {
		ctx := ctxWithUser(uuid.New(), false)
		assert.False(t, srv.isGroupOwner(ctx, uuid.New()))
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
