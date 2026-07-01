package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"net/http"
	"testing"
)

func TestListSessions(t *testing.T) {
	t.Parallel()
	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodGet, testQueryDeviceID+uuid.New().String(), "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("missing_device_id", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodGet, testPathSessions, token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid_device_id", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodGet, testQueryDeviceID+"not-a-uuid", token, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodGet, testQueryDeviceID+uuid.New().String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var sessions []*db.AgentSession
		require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
		assert.Empty(t, sessions)
	})

	t.Run("returns_sessions", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := testTenantContext(t)

		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)

		testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)
		testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		w := doRequest(srv, http.MethodGet, testQueryDeviceID+device.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var sessions []*db.AgentSession
		require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
		assert.Len(t, sessions, 2)
	})

	t.Run("only_returns_for_given_device", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := testTenantContext(t)

		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device1 := testutil.SeedDevice(t, ctx, srv.store, group.ID)
		device2 := testutil.SeedDevice(t, ctx, srv.store, group.ID)

		testutil.SeedAgentSession(t, ctx, srv.store, device1.ID, user.ID)
		testutil.SeedAgentSession(t, ctx, srv.store, device2.ID, user.ID)

		w := doRequest(srv, http.MethodGet, testQueryDeviceID+device1.ID.String(), token, nil)
		assert.Equal(t, http.StatusOK, w.Code)

		var sessions []*db.AgentSession
		require.NoError(t, json.NewDecoder(w.Body).Decode(&sessions))
		assert.Len(t, sessions, 1)
		assert.Equal(t, device1.ID, sessions[0].DeviceID)
	})
}
