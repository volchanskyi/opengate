package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"io"
	"net/http"
	"nhooyr.io/websocket"
	"strings"
	"testing"
)

// connectAgent establishes a QUIC agent and returns the stream and device ID.
// Reuses the pattern from agentapi_test.go.
func (e *sessionTestEnv) connectAgent(t *testing.T, groupID uuid.UUID) (io.ReadWriter, uuid.UUID) {
	t.Helper()
	// Create a temporary agentTestEnv-like setup reusing the existing env
	ae := &agentTestEnv{
		store:   e.store,
		devices: e.devices,
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
