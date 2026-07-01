package api

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsGroupOwner(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	owner, ownerToken := seedTestUser(t, srv, cfg, "owner@example.com", false)
	_ = ownerToken

	// Create a group owned by the user.
	groupID := uuid.New()
	err := srv.groups.Create(testTenantContext(t), &device.Group{
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
			OrgID:   dbtx.DefaultOrgID,
		}
		ctx := context.WithValue(t.Context(), claimsKey, claims)
		return dbtx.WithDefaultTenant(ctx, admin)
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
