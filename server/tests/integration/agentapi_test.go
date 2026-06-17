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
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
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
		DeviceLogs:    testutil.NewTestLogs(t, store),
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

// connectAgentWithID establishes a QUIC connection as a test agent with a specific device ID.
// The device must already exist in the DB.
func (e *agentTestEnv) connectAgentWithID(t *testing.T, deviceID uuid.UUID) *quic.Stream {
	t.Helper()
	ctx := context.Background()

	tlsCert, err := e.certMgr.SignAgent(deviceID.String(), "test-agent")
	require.NoError(t, err)

	agentTLSCfg := e.certMgr.AgentTLSConfig(tlsCert)

	conn, err := quic.DialAddr(ctx, e.addr, agentTLSCfg, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "test done") })

	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)

	performClientHandshake(t, stream, tlsCert.Certificate[0])
	sendAgentRegister(t, stream)

	return stream
}

// connectAgent establishes a QUIC connection as a test agent and performs the handshake.
// Returns the stream, device ID, and agent cert DER.
func (e *agentTestEnv) connectAgent(t *testing.T, groupID uuid.UUID) (*quic.Stream, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	deviceID := uuid.New()

	// Pre-seed the device in the DB BEFORE connecting so the server can
	// find its group during accept(). Previously this happened after the
	// handshake, causing races with concurrent connections.
	seedDevice := &device.Device{
		ID:       deviceID,
		GroupID:  groupID,
		Hostname: "pre-seed",
		OS:       "linux",
		Status:   db.StatusOffline,
	}
	require.NoError(t, e.devices.Upsert(ctx, seedDevice))

	// Sign an agent cert using the CA
	tlsCert, err := e.certMgr.SignAgent(deviceID.String(), "test-agent")
	require.NoError(t, err)

	agentTLSCfg := e.certMgr.AgentTLSConfig(tlsCert)

	// Connect via QUIC
	conn, err := quic.DialAddr(ctx, e.addr, agentTLSCfg, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseWithError(0, "test done") })

	// Open control stream (agent-initiated): the agent opens and writes
	// first, per RFC 9000 stream-discovery.
	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)

	// Client-initiated stream IDs are even (RFC 9000 §2.1).
	require.Zero(t, int64(stream.StreamID())%2, "control stream must be client-initiated (even ID)")

	performClientHandshake(t, stream, tlsCert.Certificate[0])
	sendAgentRegister(t, stream)

	return stream, deviceID
}

// getDevice fetches a device by ID, failing the test on error.
func getDevice(t *testing.T, env *agentTestEnv, deviceID uuid.UUID) *device.Device {
	t.Helper()
	d, err := env.devices.Get(context.Background(), deviceID)
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
		d, err := env.devices.Get(context.Background(), deviceID)
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
