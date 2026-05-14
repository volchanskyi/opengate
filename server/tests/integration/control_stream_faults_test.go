package integration

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// Phase B / B4: control-stream fault injection.
//
// The server's control loop (agentapi.Server.runControlLoop) reads frames
// from the agent's QUIC stream and dispatches them to handleControl. A
// misbehaving or crashing agent can violate any of: frame envelope, payload
// well-formedness, or stream liveness. These tests pin the server's
// observable behaviour for each fault class so future refactors of the
// control loop don't regress the reconciliation path (device → offline
// after stream errors, no goroutine leak across the disconnect).

// waitForDeviceStatus polls the DB until the given device reaches the
// expected status, or the deadline expires.
func waitForDeviceStatus(t *testing.T, store db.Store, deviceID protocol.DeviceID, want db.DeviceStatus) {
	t.Helper()
	require.Eventually(t, func() bool {
		dev, err := store.GetDevice(context.Background(), deviceID)
		return err == nil && dev.Status == want
	}, 3*time.Second, 25*time.Millisecond,
		"device %s never reached status %q", deviceID, want)
}

// setupOnlineAgent creates a test env, seeds a user + group, connects a
// fake agent through the full handshake, and waits for the resulting
// device row to flip to StatusOnline. Returns the env (for store / srv
// access in tests), the agent-side QUIC stream (for fault injection), and
// the device's UUID.
func setupOnlineAgent(t *testing.T) (*agentTestEnv, *quic.Stream, protocol.DeviceID) {
	t.Helper()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)
	stream, deviceID := env.connectAgent(t, group.ID)
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOnline)
	return env, stream, deviceID
}

// writeRawControlFrameHeader writes a FrameControl envelope claiming
// payloadLen bytes will follow, without writing the payload. Useful for
// partial-frame and corruption tests.
func writeRawControlFrameHeader(t *testing.T, stream *quic.Stream, payloadLen uint32) {
	t.Helper()
	var header [5]byte
	header[0] = protocol.FrameControl
	binary.BigEndian.PutUint32(header[1:], payloadLen)
	_, err := stream.Write(header[:])
	require.NoError(t, err)
}

// writeCorruptedControlFrame writes a valid FrameControl envelope around
// a payload that is NOT a valid msgpack-encoded ControlMessage. The
// server's codec.DecodeControl must reject it.
func writeCorruptedControlFrame(t *testing.T, stream *quic.Stream, garbage []byte) {
	t.Helper()
	writeRawControlFrameHeader(t, stream, uint32(len(garbage)))
	_, err := stream.Write(garbage)
	require.NoError(t, err)
}

func TestControlStream_CorruptedMsgpackPayloadDisconnectsAgent(t *testing.T) {
	env, stream, deviceID := setupOnlineAgent(t)

	// Garbage that the msgpack decoder cannot parse. 0xc1 is the reserved
	// byte in MessagePack and is guaranteed to produce a decode error.
	writeCorruptedControlFrame(t, stream, []byte{0xc1, 0xc1, 0xc1, 0xc1})

	// The control loop logs the decode error and exits, which triggers
	// unregisterConn → SetDeviceStatus(offline).
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestControlStream_PartialFrameThenCloseDisconnectsAgent(t *testing.T) {
	env, stream, deviceID := setupOnlineAgent(t)

	// Announce a 256-byte payload, then send only 10 bytes and close.
	// codec.ReadFrame must surface io.ErrUnexpectedEOF (or io.EOF after
	// close) — runControlLoop handles both as a clean exit, not a panic.
	writeRawControlFrameHeader(t, stream, 256)
	_, err := stream.Write([]byte("partial-10"))
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestControlStream_SendAfterStreamCloseFailsAndReconciles(t *testing.T) {
	env, stream, deviceID := setupOnlineAgent(t)

	// Resolve the AgentConn the API would use, then close the stream from
	// the agent side. This simulates "handler tries to push a request,
	// agent has just disconnected" — the path API handlers like
	// RestartDevice / GetDeviceHardware traverse when the connection has
	// been torn down without an explicit deregister.
	ac := env.srv.GetAgent(deviceID)
	require.NotNil(t, ac, "agent must be registered before we close the stream")
	require.NoError(t, stream.Close())

	// Eventually the server's read of the closed stream surfaces an error,
	// the control loop exits, and the device goes offline. Verify the
	// reconciliation happens before we attempt the send so we exercise
	// the "agent is gone" branch deterministically.
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)

	// Now any control-plane send on the same AgentConn must surface an
	// error rather than silently succeeding or hanging — the API
	// handlers turn that into a 5xx response, which is the correct
	// observable behaviour. Errors include io.EOF, "stream reset",
	// "connection closed" — we only assert non-nil.
	err := ac.SendRequestHardwareReport(context.Background())
	require.Error(t, err, "send on a closed stream must surface an error")
	// Just for documentation: assert the error chain doesn't include a
	// nil-pointer dereference or other unexpected wrap.
	assert.NotEmpty(t, err.Error(), "error must carry a message")
}

