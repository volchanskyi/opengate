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
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	// Garbage that the msgpack decoder cannot parse. 0xc1 is the reserved
	// byte in MessagePack and is guaranteed to produce a decode error.
	writeCorruptedControlFrame(t, stream, []byte{0xc1, 0xc1, 0xc1, 0xc1})

	// The control loop logs the decode error and exits, which triggers
	// unregisterConn → SetDeviceStatus(offline).
	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestControlStream_PartialFrameThenCloseDisconnectsAgent(t *testing.T) {
	t.Parallel()
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

// TestControlStream_ConcurrentServerInitiatedSendsArriveDecodable was deferred
// during Phase B / B5 because it exposed a race in AgentConn.sendControl: the
// codec's WriteFrame issues a 5-byte header write followed by an N-byte
// payload write, and two concurrent server-initiated sends could interleave
// (header A, header B, payload A, payload B) — corrupting the envelope on the
// agent side. Phase C / C3 closes the race by adding a write mutex inside
// AgentConn; this test pins the contract so future refactors don't regress.
func TestControlStream_ConcurrentServerInitiatedSendsArriveDecodable(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	ac := env.srv.GetAgent(deviceID)
	require.NotNil(t, ac, "agent must be registered before issuing concurrent sends")

	// Fire two server-initiated control sends concurrently. Without the
	// write mutex, the (header, payload) pairs can interleave on the stream.
	errCh := make(chan error, 2)
	go func() { errCh <- ac.SendRequestHardwareReport(context.Background()) }()
	go func() { errCh <- ac.SendRequestDeviceLogs(context.Background(), db.LogFilter{}) }()
	for i := 0; i < 2; i++ {
		require.NoError(t, <-errCh, "concurrent send %d returned an error", i)
	}

	// Read both frames from the agent side. Both must decode cleanly and
	// each message type must appear exactly once.
	codec := &protocol.Codec{}
	seen := map[protocol.ControlMessageType]int{}
	for i := 0; i < 2; i++ {
		require.NoError(t, stream.SetReadDeadline(time.Now().Add(3*time.Second)))
		frameType, payload, err := codec.ReadFrame(stream)
		require.NoError(t, err, "frame %d ReadFrame failed (envelope corruption?)", i)
		require.Equal(t, protocol.FrameControl, frameType, "frame %d wrong type", i)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err, "frame %d DecodeControl failed (payload corruption?)", i)
		seen[msg.Type]++
	}
	assert.Equal(t, 1, seen[protocol.MsgRequestHardwareReport], "RequestHardwareReport should appear exactly once")
	assert.Equal(t, 1, seen[protocol.MsgRequestDeviceLogs], "RequestDeviceLogs should appear exactly once")
}

func TestControlStream_SendAfterStreamCloseFailsAndReconciles(t *testing.T) {
	t.Parallel()
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

