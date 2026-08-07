package session_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// TestPostgres_DeleteRelaySession_CrossTenant covers relay teardown, which runs
// after both request contexts are gone and therefore has no request tenant.
func TestPostgres_DeleteRelaySession_CrossTenant(t *testing.T) {
	t.Parallel()
	store := testutil.NewTestStore(t)
	repo := testutil.NewTestSessions(t, store)
	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, true)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	userB := testutil.SeedUser(t, ctxB, store)
	groupB := testutil.SeedSite(t, ctxB, store)
	deviceB := testutil.SeedDevice(t, ctxB, store, groupB.ID)
	sessionB := testutil.SeedAgentSession(t, ctxB, store, deviceB.ID, userB.ID)

	require.NoError(t, repo.DeleteRelaySession(context.Background(), sessionB.Token))
	_, err := repo.Get(ctxB, sessionB.Token)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)

	err = repo.DeleteRelaySession(context.Background(), sessionB.Token)
	assert.ErrorIs(t, err, session.ErrSessionNotFound)
}
