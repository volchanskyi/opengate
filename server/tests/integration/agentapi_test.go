package integration

import (
	"context"
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
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// agentTestEnv sets up a real in-process agentapi server for integration tests.
type agentTestEnv struct {
	store   db.Store
	certMgr *cert.Manager
	srv     *agentapi.AgentServer
	addr    string
	cancel  context.CancelFunc
}

func newAgentTestEnv(t *testing.T) *agentTestEnv {
	t.Helper()

	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	r := relay.NewRelay()
	logger := testLogger()
	srv := agentapi.NewAgentServer(cm, store, r, &notifications.NoopNotifier{}, logger)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	// Wait for the server to be listening and get the actual address.
	actualAddr := srv.Addr()

	t.Cleanup(func() {
		cancel()
		time.Sleep(50 * time.Millisecond)
	})

	return &agentTestEnv{
		store:   store,
		certMgr: cm,
		srv:     srv,
		addr:    actualAddr,
		cancel:  cancel,
	}
}

// connectAgentWithID establishes a QUIC connection as a test agent with a specific device ID.
// The device must already exist in the DB.
func (e *agentTestEnv) connectAgentWithID(t *testing.T, deviceID uuid.UUID) quic.Stream {
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

	stream, err := conn.AcceptStream(ctx)
	require.NoError(t, err)

	serverHello := make([]byte, 81)
	_, err = io.ReadFull(stream, serverHello)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), serverHello[0])

	agentCertDER := tlsCert.Certificate[0]
	agentCertHash := sha512.Sum384(agentCertDER)
	var nonce [32]byte
	copy(nonce[:], serverHello[1:33])
	agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
	_, err = stream.Write(agentHello)
	require.NoError(t, err)

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

	return stream
}

// connectAgent establishes a QUIC connection as a test agent and performs the handshake.
// Returns the stream, device ID, and agent cert DER.
func (e *agentTestEnv) connectAgent(t *testing.T, groupID uuid.UUID) (quic.Stream, uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	deviceID := uuid.New()

	// Pre-seed the device in the DB BEFORE connecting so the server can
	// find its group during accept(). Previously this happened after the
	// handshake, causing races with concurrent connections on SQLite.
	seedDevice := &db.Device{
		ID:       deviceID,
		GroupID:  groupID,
		Hostname: "pre-seed",
		OS:       "linux",
		Status:   db.StatusOffline,
	}
	require.NoError(t, e.store.UpsertDevice(ctx, seedDevice))

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

	// Accept control stream (server-initiated)
	stream, err := conn.AcceptStream(ctx)
	require.NoError(t, err)

	// Read ServerHello
	serverHello := make([]byte, 81)
	_, err = io.ReadFull(stream, serverHello)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), serverHello[0])

	// Send AgentHello
	agentCertDER := tlsCert.Certificate[0]
	agentCertHash := sha512.Sum384(agentCertDER)
	var nonce [32]byte
	copy(nonce[:], serverHello[1:33])
	agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
	_, err = stream.Write(agentHello)
	require.NoError(t, err)

	// Send AgentRegister
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
	err = codec.WriteFrame(stream, protocol.FrameControl, payload)
	require.NoError(t, err)

	return stream, deviceID
}

func TestAgentConnect_RegistersDevice(t *testing.T) {
	env := newAgentTestEnv(t)
	ctx := context.Background()

	// Create a group for the device
	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	_, deviceID := env.connectAgent(t, group.ID)

	// Verify device appears in DB as online
	require.Eventually(t, func() bool {
		device, err := env.store.GetDevice(ctx, deviceID)
		if err != nil {
			return false
		}
		return device.Status == db.StatusOnline && device.Hostname == "integration-test-host"
	}, 3*time.Second, 50*time.Millisecond)
}

func TestAgentConnect_HeartbeatUpdatesLastSeen(t *testing.T) {
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	stream, deviceID := env.connectAgent(t, group.ID)

	// Wait for registration to complete
	time.Sleep(100 * time.Millisecond)

	// Record current last_seen
	device, err := env.store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	originalLastSeen := device.UpdatedAt

	// Send heartbeat
	codec := &protocol.Codec{}
	hbMsg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	}
	payload, err := codec.EncodeControl(hbMsg)
	require.NoError(t, err)
	err = codec.WriteFrame(stream, protocol.FrameControl, payload)
	require.NoError(t, err)

	// Verify last_seen updated
	require.Eventually(t, func() bool {
		device, err := env.store.GetDevice(ctx, deviceID)
		if err != nil {
			return false
		}
		return device.UpdatedAt.After(originalLastSeen) || device.UpdatedAt.Equal(originalLastSeen)
	}, 2*time.Second, 50*time.Millisecond)

	// Verify still online
	device, err = env.store.GetDevice(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOnline, device.Status)
}

func TestAgentConnect_DisconnectSetsOffline(t *testing.T) {
	env := newAgentTestEnv(t)
	ctx := context.Background()

	user := testutil.SeedUser(t, ctx, env.store)
	group := testutil.SeedGroup(t, ctx, env.store, user.ID)

	stream, deviceID := env.connectAgent(t, group.ID)

	// Wait for registration
	require.Eventually(t, func() bool {
		device, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && device.Status == db.StatusOnline
	}, 2*time.Second, 50*time.Millisecond)

	// Close the stream to disconnect
	stream.Close()

	// Verify device becomes offline
	require.Eventually(t, func() bool {
		device, err := env.store.GetDevice(ctx, deviceID)
		return err == nil && device.Status == db.StatusOffline
	}, 5*time.Second, 100*time.Millisecond)
}
