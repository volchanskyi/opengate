package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// TestDenyIfNotAdmin covers the mutation boundary helper every configuration
// handler shares: admins pass through, everyone else gets the caller's
// forbidden response.
func TestDenyIfNotAdmin(t *testing.T) {
	t.Parallel()

	ctxWithUser := func(admin bool) context.Context {
		claims := &auth.Claims{
			UserID:   uuid.New(),
			Email:    "test@test.com",
			IsAdmin:  admin,
			TenantID: dbtx.DefaultTenantID,
		}
		return context.WithValue(t.Context(), claimsKey, claims)
	}

	forbidden := DeleteDevice403JSONResponse{Error: msgAdminRequired}

	t.Run("admin passes", func(t *testing.T) {
		resp, denied := denyIfNotAdmin(ctxWithUser(true), forbidden)
		assert.False(t, denied)
		assert.Zero(t, resp)
	})

	t.Run("non-admin denied", func(t *testing.T) {
		resp, denied := denyIfNotAdmin(ctxWithUser(false), forbidden)
		assert.True(t, denied)
		assert.Equal(t, forbidden, resp)
	})

	t.Run("unauthenticated denied", func(t *testing.T) {
		_, denied := denyIfNotAdmin(t.Context(), forbidden)
		assert.True(t, denied)
	})
}

func TestContextHelpers(t *testing.T) {
	t.Parallel()
	t.Run("ContextClaims returns nil for empty context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Nil(t, ContextClaims(req.Context()))
	})

	t.Run("ContextUserID returns Nil for empty context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		assert.Equal(t, uuid.Nil, ContextUserID(req.Context()))
	})
}
