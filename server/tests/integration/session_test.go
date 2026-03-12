package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
)

const (
	pathSessions = "/api/v1/sessions"
	bearerPrefix = "Bearer "
)

// sessionTestEnv bundles all dependencies for session integration tests.
type sessionTestEnv struct {
	store     db.Store
	certMgr   *cert.Manager
	relay     *relay.Relay
	agentSrv  *agentapi.AgentServer
	agentAddr string
	httpSrv   *httptest.Server
	jwt       *auth.JWTConfig
	cancel    context.CancelFunc
}

func newSessionTestEnv(t *testing.T) *sessionTestEnv {
	t.Helper()

	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	r := relay.NewRelay()
	logger := testLogger()
	agentSrv := agentapi.NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, logger)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		agentSrv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	agentAddr := agentSrv.Addr() // wait for QUIC to be ready

	jwtCfg := &auth.JWTConfig{
		Secret:   "integration-test-secret-32-bytes!",
		Issuer:   "opengate-integration",
		Duration: 15 * time.Minute,
	}

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
	apiSrv := api.NewServer(api.ServerConfig{
		Store:     store,
		JWT:       jwtCfg,
		Agents:    agentSrv,
		Relay:     r,
		Signaling: sigTracker,
		Notifier:  &notifications.NoopNotifier{},
		Logger:    logger,
	})
	ts := httptest.NewServer(apiSrv)

	t.Cleanup(func() {
		ts.Close()
		cancel()
		time.Sleep(50 * time.Millisecond)
	})

	return &sessionTestEnv{
		store:     store,
		certMgr:   cm,
		relay:     r,
		agentSrv:  agentSrv,
		agentAddr: agentAddr,
		httpSrv:   ts,
		jwt:       jwtCfg,
		cancel:    cancel,
	}
}

// connectAgent establishes a QUIC agent and returns the stream and device ID.
// Reuses the pattern from agentapi_test.go.
func (e *sessionTestEnv) connectAgent(t *testing.T, groupID uuid.UUID) (io.ReadWriter, uuid.UUID) {
	t.Helper()
	// Create a temporary agentTestEnv-like setup reusing the existing env
	ae := &agentTestEnv{
		store:   e.store,
		certMgr: e.certMgr,
		srv:     e.agentSrv,
		addr:    e.agentAddr,
	}
	stream, deviceID := ae.connectAgent(t, groupID)
	return stream, deviceID
}

type createSessionResp struct {
	Token    string `json:"token"`
	RelayURL string `json:"relay_url"`
}

func (e *sessionTestEnv) createSession(t *testing.T, jwt string, deviceID uuid.UUID, perms map[string]bool) createSessionResp {
	t.Helper()
	body := map[string]interface{}{
		"device_id": deviceID.String(),
	}
	if perms != nil {
		body["permissions"] = perms
	}

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))

	req, err := http.NewRequest(http.MethodPost, e.httpSrv.URL+pathSessions, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := e.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var result createSessionResp
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}

func (e *sessionTestEnv) listSessions(t *testing.T, jwt string, deviceID uuid.UUID) []*db.AgentSession {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, e.httpSrv.URL+pathSessions+"?device_id="+deviceID.String(), nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := e.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var sessions []*db.AgentSession
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&sessions))
	return sessions
}

func (e *sessionTestEnv) deleteSession(t *testing.T, jwt, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, e.httpSrv.URL+pathSessions+"/"+token, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := e.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

func (e *sessionTestEnv) dialRelayWS(t *testing.T, ctx context.Context, token, side, jwt string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(e.httpSrv.URL, "http") + "/ws/relay/" + token + "?side=" + side
	headers := http.Header{}
	if jwt != "" {
		headers.Set("Authorization", bearerPrefix+jwt)
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	return conn
}

func TestSessionLifecycle_CreateAndRelay(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// 1. Connect QUIC agent
	stream, deviceID := env.connectAgent(t, group.ID)

	// Wait for agent to register as online
	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// 2. Create session via REST API
	result := env.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})
	assert.Len(t, result.Token, 64)
	assert.Contains(t, result.RelayURL, "/ws/relay/"+result.Token)

	// 3. Read SessionRequest from QUIC control stream
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, frameType)

	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSessionRequest, msg.Type)
	assert.Equal(t, protocol.SessionToken(result.Token), msg.Token)
	require.NotNil(t, msg.Permissions)
	assert.True(t, msg.Permissions.Desktop)

	// 4. Agent sends SessionAccept
	acceptMsg := &protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	}
	acceptPayload, err := codec.EncodeControl(acceptMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	// 5. Browser connects to relay
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	browserConn := env.dialRelayWS(t, wsCtx, result.Token, "browser", jwtToken)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	// 6. Agent connects to relay
	agentConn := env.dialRelayWS(t, wsCtx, result.Token, "agent", "")
	defer agentConn.Close(websocket.StatusNormalClosure, "")

	// Wait for relay to wire both sides
	time.Sleep(200 * time.Millisecond)

	// 7. Agent sends test payload → browser receives it
	require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, []byte("agent-payload")))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("agent-payload"), data)

	// 8. Browser sends test payload → agent receives it
	require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, []byte("browser-payload")))
	_, data, err = agentConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("browser-payload"), data)

	// 9. Delete session
	status := env.deleteSession(t, jwtToken, result.Token)
	assert.Equal(t, http.StatusNoContent, status)
}

func TestSessionLifecycle_AgentRejectsSession(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := env.createSession(t, jwtToken, deviceID, nil)

	// Read SessionRequest
	codec := &protocol.Codec{}
	_, _, err = codec.ReadFrame(stream)
	require.NoError(t, err)

	// Agent rejects session
	rejectMsg := &protocol.ControlMessage{
		Type:   protocol.MsgSessionReject,
		Token:  protocol.SessionToken(result.Token),
		Reason: "busy",
	}
	rejectPayload, err := codec.EncodeControl(rejectMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, rejectPayload))

	// Relay should have no active sessions
	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, 0, env.relay.ActiveSessionCount())
}

func TestSessionLifecycle_MultipleSessionsSameDevice(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	_, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// Create 2 sessions
	result1 := env.createSession(t, jwtToken, deviceID, nil)
	result2 := env.createSession(t, jwtToken, deviceID, nil)

	// List sessions should return 2
	sessions := env.listSessions(t, jwtToken, deviceID)
	assert.Len(t, sessions, 2)

	// Delete one
	status := env.deleteSession(t, jwtToken, result1.Token)
	assert.Equal(t, http.StatusNoContent, status)

	sessions = env.listSessions(t, jwtToken, deviceID)
	assert.Len(t, sessions, 1)

	// Delete the other
	status = env.deleteSession(t, jwtToken, result2.Token)
	assert.Equal(t, http.StatusNoContent, status)

	sessions = env.listSessions(t, jwtToken, deviceID)
	assert.Empty(t, sessions)
}

func TestSessionLifecycle_ConcurrentSessions(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	// Connect 3 agents
	type agentInfo struct {
		stream   io.ReadWriter
		deviceID uuid.UUID
	}
	agents := make([]agentInfo, 3)
	for i := range agents {
		stream, deviceID := env.connectAgent(t, group.ID)
		agents[i] = agentInfo{stream: stream, deviceID: deviceID}
	}

	// Wait for all to register
	for _, a := range agents {
		require.Eventually(t, func() bool {
			d, err := env.store.GetDevice(ctx, a.deviceID)
			return err == nil && d.Status == db.StatusOnline
		}, 3*time.Second, 50*time.Millisecond)
	}

	// Create sessions for all 3 simultaneously
	type sessionResult struct {
		token    string
		deviceID uuid.UUID
	}
	results := make([]sessionResult, 3)
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, deviceID uuid.UUID) {
			defer wg.Done()
			r := env.createSession(t, jwtToken, deviceID, nil)
			results[i] = sessionResult{token: r.Token, deviceID: deviceID}
		}(i, a.deviceID)
	}
	wg.Wait()

	// All 3 sessions exist
	for _, r := range results {
		assert.Len(t, r.token, 64)
	}

	// Each relay pair exchanges data concurrently
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	var wg2 sync.WaitGroup
	for i, r := range results {
		wg2.Add(1)
		go func(i int, token string) {
			defer wg2.Done()

			agentConn := env.dialRelayWS(t, wsCtx, token, "agent", "")
			defer agentConn.Close(websocket.StatusNormalClosure, "")

			browserConn := env.dialRelayWS(t, wsCtx, token, "browser", jwtToken)
			defer browserConn.Close(websocket.StatusNormalClosure, "")

			time.Sleep(200 * time.Millisecond)

			payload := []byte("test-" + token[:8])
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
			_, data, err := browserConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, payload, data)
		}(i, r.token)
	}
	wg2.Wait()
}

func TestSessionLifecycle_AgentDisconnectDuringSession(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := env.createSession(t, jwtToken, deviceID, nil)

	// Read SessionRequest and accept
	codec := &protocol.Codec{}
	_, _, err = codec.ReadFrame(stream)
	require.NoError(t, err)

	acceptMsg := &protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	}
	acceptPayload, err := codec.EncodeControl(acceptMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	// Connect both sides to relay
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	agentWSConn := env.dialRelayWS(t, wsCtx, result.Token, "agent", "")
	browserConn := env.dialRelayWS(t, wsCtx, result.Token, "browser", jwtToken)
	defer browserConn.Close(websocket.StatusNormalClosure, "")

	time.Sleep(200 * time.Millisecond)

	// Verify data flows
	require.NoError(t, agentWSConn.Write(wsCtx, websocket.MessageBinary, []byte("pre-disconnect")))
	_, data, err := browserConn.Read(wsCtx)
	require.NoError(t, err)
	assert.Equal(t, []byte("pre-disconnect"), data)

	// Close agent WebSocket
	agentWSConn.Close(websocket.StatusNormalClosure, "")

	// Browser should get an error
	readCtx, readCancel := context.WithTimeout(ctx, 3*time.Second)
	defer readCancel()
	_, _, err = browserConn.Read(readCtx)
	assert.Error(t, err)

	// Relay active count should drop
	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 5*time.Second, 100*time.Millisecond)
}
