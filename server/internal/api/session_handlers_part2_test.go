package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

type createSessionEnv struct {
	srv         *Server
	deviceID    uuid.UUID
	userID      uuid.UUID
	token       string
	agentStream *bytes.Buffer
}

func TestCreateSessionUnauthenticated(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	w := doRequest(srv, http.MethodPost, testPathSessions, "", map[string]string{"device_id": uuid.New().String()})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateSessionInvalidJSON(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testEmailSess, false)
	w := doRawRequest(srv, http.MethodPost, testPathSessions, token, "not-json")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSessionInvalidDeviceID(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testEmailSess, false)
	w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{"device_id": "not-a-uuid"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateSessionDeviceNotFound(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	_, token := seedTestUser(t, srv, cfg, testEmailSess, false)
	w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{"device_id": uuid.New().String()})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCreateSessionAgentNotConnected(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServer(t)
	user, token := seedTestUser(t, srv, cfg, testEmailSess, false)
	ctx := testTenantContext(t)
	group := testutil.SeedGroup(t, ctx, srv.store, user.ID)
	d := testutil.SeedDevice(t, ctx, srv.store, group.ID)

	w := doRequest(srv, http.MethodPost, testPathSessions, token, map[string]string{"device_id": d.ID.String()})
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestCreateSessionSuccess(t *testing.T) {
	t.Parallel()
	env := newCreateSessionEnv(t)
	w := createSession(t, env, nil, nil)
	assert.Equal(t, http.StatusCreated, w.Code)

	resp := decodeSessionResponse(t, w)
	assert.Len(t, resp["token"], 64)
	assert.Contains(t, resp["relay_url"], testPathWSRelay+resp["token"])
	assertSessionPersisted(t, env, resp["token"])
	assertSessionRequestSent(t, env, resp["token"], nil)
}

func TestCreateSessionRelayURLScheme(t *testing.T) {
	t.Parallel()
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
			env := newCreateSessionEnv(t)
			headers := map[string]string{}
			if tt.forwardedProto != "" {
				headers["X-Forwarded-Proto"] = tt.forwardedProto
			}
			w := createSession(t, env, nil, headers)
			assert.Equal(t, http.StatusCreated, w.Code)
			assert.Contains(t, decodeSessionResponse(t, w)["relay_url"], tt.wantScheme)
		})
	}
}

func TestCreateSessionDefaultPermissions(t *testing.T) {
	t.Parallel()
	env := newCreateSessionEnv(t)
	w := createSession(t, env, nil, nil)
	assert.Equal(t, http.StatusCreated, w.Code)
	assertSessionRequestSent(t, env, decodeSessionResponse(t, w)["token"], &protocol.Permissions{Desktop: true, Terminal: true})
}

func TestCreateSessionCustomPermissions(t *testing.T) {
	t.Parallel()
	env := newCreateSessionEnv(t)
	perms := map[string]bool{"desktop": true, "terminal": true}
	w := createSession(t, env, perms, nil)
	assert.Equal(t, http.StatusCreated, w.Code)
	assertSessionRequestSent(t, env, decodeSessionResponse(t, w)["token"], &protocol.Permissions{Desktop: true, Terminal: true})
}

func newCreateSessionEnv(t *testing.T) createSessionEnv {
	t.Helper()
	var agentStream bytes.Buffer
	store := testutil.NewTestStore(t)
	ctx := testTenantContext(t)
	user := testutil.SeedUser(t, ctx, store)
	group := testutil.SeedGroup(t, ctx, store, user.ID)
	d := testutil.SeedDevice(t, ctx, store, group.ID)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ac := agentapi.NewAgentConn(agentapi.AgentConnConfig{DeviceID: d.ID, GroupID: group.ID, Stream: &agentStream, Devices: testutil.NewTestDevices(t, store), Hardware: testutil.NewTestHardware(t, store), DeviceUpdates: testutil.NewTestDeviceUpdates(t, store), Logger: logger})
	lookup := &stubAgentGetter{agents: map[protocol.DeviceID]AgentControl{d.ID: ac}}
	srv, cfg := newTestServerWithStoreAndAgents(t, store, lookup, relay.NewRelay(slog.Default()))
	jwtToken, err := cfg.GenerateToken(user.ID, user.Email, user.IsAdmin, user.OrgID)
	require.NoError(t, err)
	return createSessionEnv{srv: srv, deviceID: d.ID, userID: user.ID, token: jwtToken, agentStream: &agentStream}
}

func createSession(t *testing.T, env createSessionEnv, permissions map[string]bool, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	body := map[string]interface{}{"device_id": env.deviceID.String()}
	if permissions != nil {
		body["permissions"] = permissions
	}
	return doRequestWithHeaders(env.srv, http.MethodPost, testPathSessions, env.token, body, headers)
}

func decodeSessionResponse(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	return resp
}

func assertSessionPersisted(t *testing.T, env createSessionEnv, token string) {
	t.Helper()
	sess, err := testutil.NewTestSessions(t, env.srv.store).Get(testTenantContext(t), token)
	require.NoError(t, err)
	assert.Equal(t, env.deviceID, sess.DeviceID)
	assert.Equal(t, env.userID, sess.UserID)
}

func assertSessionRequestSent(t *testing.T, env createSessionEnv, token string, want *protocol.Permissions) {
	t.Helper()
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(env.agentStream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSessionRequest, msg.Type)
	assert.Equal(t, protocol.SessionToken(token), msg.Token)
	assert.Contains(t, msg.RelayURL, testPathWSRelay+token)
	if want != nil {
		require.NotNil(t, msg.Permissions)
		assert.Equal(t, want.Desktop, msg.Permissions.Desktop)
		assert.Equal(t, want.Terminal, msg.Permissions.Terminal)
	}
}
