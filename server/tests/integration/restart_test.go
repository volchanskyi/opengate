package integration

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// restartDevice posts a raw restart body and returns the response status.
func restartDevice(t *testing.T, env *sessionTestEnv, jwt string, deviceID uuid.UUID, body string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost,
		env.httpSrv.URL+"/api/v1/devices/"+deviceID.String()+"/restart",
		bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", bearerPrefix+jwt)

	resp, err := env.httpSrv.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

// readControlFrame reads one control frame off the agent's QUIC stream and
// decodes it the way the agent would.
func readControlFrame(t *testing.T, stream io.ReadWriter) *protocol.ControlMessage {
	t.Helper()
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

// TestRestartDevice_ReachesAgentOverQUIC drives the restart endpoint against a
// real connected agent and asserts on the frame the agent actually receives.
//
// A reason that bottoms out empty encodes a map with no `reason` key, which the
// agent's decoder rejects — it would break the control stream and force a full
// reconnect while the caller saw a 200 for a restart that never happened. So the
// empty case must put *nothing* on the stream. The assertion is ordering-based
// rather than timing-based: a third restart with a distinct reason follows the
// refused one, and the next frame off the stream must be that third one. Any
// frame the refused request had written would arrive first.
func TestRestartDevice_ReachesAgentOverQUIC(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store)

	jwtToken, err := env.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := env.connectAgent(t, group.ID)
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	// A stated reason round-trips to a complete RestartAgent.
	require.Equal(t, http.StatusOK,
		restartDevice(t, env, jwtToken, deviceID, `{"reason":"scheduled maintenance"}`))

	msg := readControlFrame(t, stream)
	assert.Equal(t, protocol.MsgRestartAgent, msg.Type)
	assert.Equal(t, "scheduled maintenance", msg.Reason)

	// An empty reason is refused before anything is written.
	assert.Equal(t, http.StatusBadRequest,
		restartDevice(t, env, jwtToken, deviceID, `{"reason":""}`))
	assert.Equal(t, http.StatusBadRequest,
		restartDevice(t, env, jwtToken, deviceID, `{"reason":"   "}`))

	// The next frame on the stream is the following restart, proving neither
	// refused request wrote one.
	require.Equal(t, http.StatusOK,
		restartDevice(t, env, jwtToken, deviceID, `{"reason":"second attempt"}`))

	next := readControlFrame(t, stream)
	assert.Equal(t, protocol.MsgRestartAgent, next.Type)
	assert.Equal(t, "second attempt", next.Reason)

	// An omitted reason keeps the server default, which is never empty.
	require.Equal(t, http.StatusOK, restartDevice(t, env, jwtToken, deviceID, `{}`))

	fallback := readControlFrame(t, stream)
	assert.Equal(t, protocol.MsgRestartAgent, fallback.Type)
	assert.NotEmpty(t, fallback.Reason)
}
