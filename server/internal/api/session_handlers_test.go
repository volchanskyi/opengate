package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const (
	testEmailSess     = "sess@example.com"
	testPathSessions  = "/api/v1/sessions"
	testPathSessionsS = "/api/v1/sessions/"
	testQueryDeviceID = "/api/v1/sessions?device_id="
)

func TestCreateSession(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodPost, testPathSessions, "", map[string]string{
			"device_id": uuid.New().String(),
		})
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("invalid_json_body", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRawRequest(srv, http.MethodPost, testPathSessions, token, "not-json")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid_device_id", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{
			"device_id": "not-a-uuid",
		})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("device_not_in_db", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{
			"device_id": uuid.New().String(),
		})
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("agent_not_connected", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		group := testutil.SeedGroup(t, t.Context(), srv.store, user.ID)
		device := testutil.SeedDevice(t, t.Context(), srv.store, group.ID)

		w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{
			"device_id": device.ID.String(),
		})
		assert.Equal(t, http.StatusConflict, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		// Set up a server with a connected agent stub
		var agentStream bytes.Buffer
		store := testutil.NewTestStore(t)
		ctx := t.Context()

		user := testutil.SeedUser(t, ctx, store)
		group := testutil.SeedGroup(t, ctx, store, user.ID)
		device := testutil.SeedDevice(t, ctx, store, group.ID)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

		lookup := &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{
				device.ID: ac,
			},
		}

		srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay())
		// Re-use the same store
		srv.store = store

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		w := doRequest(srv, http.MethodPost, testPathSessions, jwtToken, map[string]string{
			"device_id": device.ID.String(),
		})
		assert.Equal(t, http.StatusCreated, w.Code)

		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

		// Token should be 64 hex chars
		assert.Len(t, resp["token"], 64)
		assert.Contains(t, resp["relay_url"], testPathWSRelay+resp["token"])

		// Session should be retrievable from DB
		sess, err := store.GetAgentSession(ctx, resp["token"])
		require.NoError(t, err)
		assert.Equal(t, device.ID, sess.DeviceID)
		assert.Equal(t, user.ID, sess.UserID)

		// Verify SessionRequest was written to agent stream
		codec := &protocol.Codec{}
		frameType, payload, err := codec.ReadFrame(&agentStream)
		require.NoError(t, err)
		assert.Equal(t, protocol.FrameControl, frameType)

		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		assert.Equal(t, protocol.MsgSessionRequest, msg.Type)
		assert.Equal(t, protocol.SessionToken(resp["token"]), msg.Token)
		assert.Contains(t, msg.RelayURL, testPathWSRelay+resp["token"])
	})

	t.Run("relay_url_scheme", func(t *testing.T) {
		tests := []struct {
			name           string
			forwardedProto string
			wantScheme     string
		}{
			{"x_forwarded_proto_https", "https", "wss://"},
			{"x_forwarded_proto_http", "http", "ws://"},
			{"no_proxy_header_plain_http", "", "ws://"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var agentStream bytes.Buffer
				store := testutil.NewTestStore(t)
				ctx := t.Context()

				user := testutil.SeedUser(t, ctx, store)
				group := testutil.SeedGroup(t, ctx, store, user.ID)
				device := testutil.SeedDevice(t, ctx, store, group.ID)

				logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
				ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

				lookup := &stubAgentGetter{
					agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
				}

				srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay())
				srv.store = store

				jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
				require.NoError(t, err)

				var headers map[string]string
				if tt.forwardedProto != "" {
					headers = map[string]string{"X-Forwarded-Proto": tt.forwardedProto}
				}

				w := doRequestWithHeaders(srv, http.MethodPost, testPathSessions, jwtToken,
					map[string]string{"device_id": device.ID.String()}, headers)
				assert.Equal(t, http.StatusCreated, w.Code)

				var resp map[string]interface{}
				require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
				relayURL, ok := resp["relay_url"].(string)
				require.True(t, ok)
				assert.True(t, len(relayURL) > 0)
				assert.Contains(t, relayURL, tt.wantScheme, "relay URL should use %s scheme", tt.wantScheme)
			})
		}
	})

	t.Run("default_permissions", func(t *testing.T) {
		var agentStream bytes.Buffer
		store := testutil.NewTestStore(t)
		ctx := t.Context()

		user := testutil.SeedUser(t, ctx, store)
		group := testutil.SeedGroup(t, ctx, store, user.ID)
		device := testutil.SeedDevice(t, ctx, store, group.ID)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

		lookup := &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}

		srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay())
		srv.store = store

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		// No permissions field in body
		w := doRequest(srv, http.MethodPost, testPathSessions, jwtToken, map[string]string{
			"device_id": device.ID.String(),
		})
		assert.Equal(t, http.StatusCreated, w.Code)

		// Verify permissions sent to agent are all false
		codec := &protocol.Codec{}
		_, payload, err := codec.ReadFrame(&agentStream)
		require.NoError(t, err)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		require.NotNil(t, msg.Permissions)
		assert.False(t, msg.Permissions.Desktop)
		assert.False(t, msg.Permissions.Terminal)
	})

	t.Run("custom_permissions", func(t *testing.T) {
		var agentStream bytes.Buffer
		store := testutil.NewTestStore(t)
		ctx := t.Context()

		user := testutil.SeedUser(t, ctx, store)
		group := testutil.SeedGroup(t, ctx, store, user.ID)
		device := testutil.SeedDevice(t, ctx, store, group.ID)

		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
		ac := agentapi.NewAgentConn(device.ID, group.ID, &agentStream, store, logger)

		lookup := &stubAgentGetter{
			agents: map[protocol.DeviceID]*agentapi.AgentConn{device.ID: ac},
		}

		srv, cfg := newTestServerWithAgents(t, lookup, relay.NewRelay())
		srv.store = store

		jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin)
		require.NoError(t, err)

		body := map[string]interface{}{
			"device_id": device.ID.String(),
			"permissions": map[string]bool{
				"desktop":  true,
				"terminal": true,
			},
		}
		w := doRequest(srv, http.MethodPost, testPathSessions, jwtToken, body)
		assert.Equal(t, http.StatusCreated, w.Code)

		codec := &protocol.Codec{}
		_, payload, err := codec.ReadFrame(&agentStream)
		require.NoError(t, err)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		require.NotNil(t, msg.Permissions)
		assert.True(t, msg.Permissions.Desktop)
		assert.True(t, msg.Permissions.Terminal)
	})
}

func TestListSessions(t *testing.T) {
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

		w := doRequest(srv, http.MethodGet, testQueryDeviceID + "not-a-uuid", token, nil)
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
		ctx := t.Context()

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
		ctx := t.Context()

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

func TestDeleteSession(t *testing.T) {
	t.Run("unauthenticated", func(t *testing.T) {
		srv, _ := newTestServer(t)
		w := doRequest(srv, http.MethodDelete, testPathSessionsS + "sometoken", "", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("not_found", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		_, token := seedTestUser(t, srv, cfg, testEmailSess, false)

		w := doRequest(srv, http.MethodDelete, testPathSessionsS + "nonexistent-token", token, nil)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("success", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := t.Context()

		group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
		device := testutil.SeedDevice(t, ctx, srv.store, group.ID)

		sess := testutil.SeedAgentSession(t, ctx, srv.store, device.ID, user.ID)

		w := doRequest(srv, http.MethodDelete, testPathSessionsS+sess.Token, token, nil)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Verify gone from DB
		_, err := srv.store.GetAgentSession(ctx, sess.Token)
		assert.ErrorIs(t, err, db.ErrNotFound)
	})

	t.Run("idempotent", func(t *testing.T) {
		srv, cfg := newTestServer(t)
		user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
		ctx := t.Context()

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
