package api

import (
	"github.com/stretchr/testify/assert"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestDeleteSession(t *testing.T) {
	t.Parallel()
	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodDelete, testPathSessionsS+"sometoken", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("not_found", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodDelete, testPathSessionsS+"nonexistent-token", token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := testTenantContext(t)

		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)

		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		w := doRequest(srv, http.MethodDelete, testPathSessionsS+sess.Token, token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify gone from DB
		_, err := srv.sessions.Get(ctx, sess.Token)
		assert.ErrorIs(t, err, session.ErrSessionNotFound)
	})

	t.Run("idempotent", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := testTenantContext(t)

		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)

		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		w := doRequest(srv, http.MethodDelete, testPathSessionsS+sess.Token, token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Second delete returns 404, not 500
		w = doRequest(srv, http.MethodDelete, testPathSessionsS+sess.Token, token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
