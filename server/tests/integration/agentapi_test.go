package integration

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// testLogger returns an error-level-only slog.Logger to keep test output quiet.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// performClientHandshake sends AgentHello (the agent opened the stream, so
// it writes first per RFC 9000 stream-discovery) and reads back ServerHello.
func performClientHandshake(t *testing.T, stream *quic.Stream, agentCertDER []byte) {
	t.Helper()

	agentCertHash := sha512.Sum384(agentCertDER)
	var nonce [32]byte
	_, err := rand.Read(nonce[:])
	require.NoError(t, err)
	agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
	_, err = stream.Write(agentHello)
	require.NoError(t, err)

	serverHello := make([]byte, 81)
	_, err = io.ReadFull(stream, serverHello)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), serverHello[0])
}

// sendAgentRegister encodes and writes a fixed test AgentRegister control frame.
func sendAgentRegister(t *testing.T, stream *quic.Stream) {
	t.Helper()

	codec := &protocol.Codec{}
	regMsg := &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal, protocol.CapHardwareInventory, protocol.CapDeviceLogs},
		Hostname:     "integration-test-host",
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	}
	payload, err := codec.EncodeControl(regMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))
}

// agentTestEnv sets up a real in-process agentapi server for integration tests.
type agentTestEnv struct {
	store   *db.PostgresStore
	devices device.Repository
	certMgr *cert.Manager
	srv     *agentapi.AgentServer
	addr    string
	cancel  context.CancelFunc
}

// newAgentTestEnv starts a real in-process agentapi QUIC server backed by a
// throwaway Postgres schema, for use by agent-connection integration tests.
func newAgentTestEnv(t *testing.T) *agentTestEnv {
	t.Helper()

	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	r := relay.NewRelay(slog.Default())
	logger := testLogger()
	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Devices:       testutil.NewTestDevices(t, store),
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         r,
		Notifier:      &notifications.NoopNotifier{},
		Logger:        logger,
	})

	ctx, cancel := context.WithCancel(context.Background())

	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Wait for the server to be listening and get the actual address.
	actualAddr := srv.Addr()

	t.Cleanup(func() {
		cancel()
		// Wait for the QUIC server goroutine to exit instead of a blind sleep.
		select {
		case <-listenDone:
		case <-time.After(2 * time.Second):
			t.Log("agent QUIC server did not exit within 2s of cancel")
		}
	})

	return &agentTestEnv{
		store:   store,
		certMgr: cm,
		srv:     srv,
		addr:    actualAddr,
		cancel:  cancel,
		devices: testutil.NewTestDevices(t, store),
	}
}

// caCertHash returns the SHA-384 of the env's CA cert — the value an agent
// caches and replays on the 0x14 fast path.
func (e *agentTestEnv) caCertHash() [48]byte {
	return sha512.Sum384(e.certMgr.CACert().Raw)
}

// seedDevice pre-creates an offline device row BEFORE the agent connects, so
// the server can resolve its group during accept() without a race.
func (e *agentTestEnv) seedDevice(t *testing.T, deviceID, groupID uuid.UUID) {
	t.Helper()
	require.NoError(t, e.devices.Upsert(defaultTenantContext(), &device.Device{
		ID:       deviceID,
		GroupID:  groupID,
		Hostname: "pre-seed",
		OS:       "linux",
		Status:   db.StatusOffline,
	}))
}

// dialAgentStream signs an agent cert for deviceID, dials QUIC, and opens the
// client-initiated control stream. Returns the stream and the agent cert DER.
func (e *agentTestEnv) dialAgentStream(t *testing.T, deviceID uuid.UUID) (*quic.Stream, []byte) {
	t.Helper()
	ctx := context.Background()

	tlsCert, err := e.certMgr.SignAgent(deviceID.String(), "test-agent")
	require.NoError(t, err)

	conn, err := quic.DialAddr(ctx, e.addr, e.certMgr.AgentTLSConfig(tlsCert), &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "test done") })

	// Agent opens the control stream and writes first (RFC 9000
	// stream-discovery); client-initiated stream IDs are even (§2.1).
	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	require.Zero(t, int64(stream.StreamID())%2, "control stream must be client-initiated (even ID)")

	return stream, tlsCert.Certificate[0]
}

// connectAgentWithID establishes a QUIC connection as a test agent with a
// specific device ID (which must already exist in the DB) via the full handshake.
func (e *agentTestEnv) connectAgentWithID(t *testing.T, deviceID uuid.UUID) *quic.Stream {
	t.Helper()
	stream, agentCertDER := e.dialAgentStream(t, deviceID)
	performClientHandshake(t, stream, agentCertDER)
	sendAgentRegister(t, stream)
	return stream
}

// connectAgent seeds a fresh device and connects a test agent via the full
// handshake. Returns the stream and device ID.
func (e *agentTestEnv) connectAgent(t *testing.T, groupID uuid.UUID) (*quic.Stream, uuid.UUID) {
	t.Helper()
	deviceID := uuid.New()
	e.seedDevice(t, deviceID, groupID)
	stream, agentCertDER := e.dialAgentStream(t, deviceID)
	performClientHandshake(t, stream, agentCertDER)
	sendAgentRegister(t, stream)
	return stream, deviceID
}

// connectAgentFastPath seeds a fresh device and connects via the 0x14
// fast path, sending SkipAuth with the given cached CA hash (no full
// handshake). Returns the stream and device ID; the caller drives registration.
func (e *agentTestEnv) connectAgentFastPath(t *testing.T, groupID uuid.UUID, cachedCAHash [48]byte) (*quic.Stream, uuid.UUID) {
	t.Helper()
	deviceID := uuid.New()
	e.seedDevice(t, deviceID, groupID)
	stream, _ := e.dialAgentStream(t, deviceID)
	_, err := stream.Write(protocol.EncodeSkipAuth(cachedCAHash))
	require.NoError(t, err)
	return stream, deviceID
}

// getDevice fetches a device by ID, failing the test on error.
func getDevice(t *testing.T, env *agentTestEnv, deviceID uuid.UUID) *device.Device {
	t.Helper()
	d, err := env.devices.Get(defaultTenantContext(), deviceID)
	require.NoError(t, err)
	return d
}

func TestAgentConnect_RegistersDevice(t *testing.T) {
	t.Parallel()
	env, _, deviceID := setupOnlineAgent(t)

	assert.Equal(t, "integration-test-host", getDevice(t, env, deviceID).Hostname)
}

func TestAgentConnect_HeartbeatUpdatesLastSeen(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	originalLastSeen := getDevice(t, env, deviceID).UpdatedAt

	// Send heartbeat
	codec := &protocol.Codec{}
	hbMsg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	}
	payload, err := codec.EncodeControl(hbMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))

	// Verify last_seen updated, still online
	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && !d.UpdatedAt.Before(originalLastSeen)
	}, 2*time.Second, 50*time.Millisecond)
	assert.Equal(t, db.StatusOnline, getDevice(t, env, deviceID).Status)
}

func TestAgentConnect_DisconnectSetsOffline(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	// Close the stream to disconnect
	stream.Close()

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

// TestAgentConnect_FastPath_ValidHashRegisters verifies the 0x14 reconnect
// path: an agent that replays the current CA hash skips the ServerHello
// exchange and still registers (Skipped path), driven end-to-end over QUIC.
func TestAgentConnect_FastPath_ValidHashRegisters(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	stream, deviceID := env.connectAgentFastPath(t, group.ID, env.caCertHash())
	sendAgentRegister(t, stream)

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOnline)
}

// TestAgentConnect_FastPath_StaleHashRejected verifies a stale cached CA hash
// is rejected: the server tears the connection down (so the agent would fall
// back to a full handshake) and the device never registers.
func TestAgentConnect_FastPath_StaleHashRejected(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	var staleHash [48]byte // all zeros — never the real CA hash
	stream, deviceID := env.connectAgentFastPath(t, group.ID, staleHash)

	// The server rejects the stale hash and closes the connection, so a read
	// on the agent stream fails rather than blocking.
	require.NoError(t, stream.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err := stream.Read(make([]byte, 1))
	require.Error(t, err, "server must reject a stale fast-path hash")

	assert.Equal(t, db.StatusOffline, getDevice(t, env, deviceID).Status)
}
