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

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// The agent opened the stream, so it writes AgentHello first (RFC 9000 stream discovery).
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

type agentTestEnv struct {
	store   *db.PostgresStore
	devices device.Repository
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

	actualAddr := srv.Addr()

	t.Cleanup(func() {
		cancel()
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

func (e *agentTestEnv) caCertHash() [48]byte {
	return sha512.Sum384(e.certMgr.CACert().Raw)
}

// The device row exists before the agent connects so accept resolves its site without a race.
func (e *agentTestEnv) seedDevice(t *testing.T, deviceID, siteID uuid.UUID) {
	t.Helper()
	require.NoError(t, e.devices.Upsert(defaultTenantContext(), &device.Device{
		ID:       deviceID,
		SiteID:   siteID,
		Hostname: "pre-seed",
		OS:       "linux",
		Status:   db.StatusOffline,
	}))
}

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

	stream, err := conn.OpenStreamSync(ctx)
	require.NoError(t, err)
	require.Zero(t, int64(stream.StreamID())%2, "control stream must be client-initiated (even ID)")

	return stream, tlsCert.Certificate[0]
}

// Registration advertises HardwareInventory, so the server always sends RequestHardwareReport.
func drainRegisterHardwareRequest(t *testing.T, stream *quic.Stream) {
	t.Helper()
	require.NoError(t, stream.SetReadDeadline(time.Now().Add(5*time.Second)))
	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(stream)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	require.Equal(t, protocol.MsgRequestHardwareReport, msg.Type)
	require.NoError(t, stream.SetReadDeadline(time.Time{}))
}

func (e *agentTestEnv) connectAgentWithID(t *testing.T, deviceID uuid.UUID) *quic.Stream {
	t.Helper()
	stream, agentCertDER := e.dialAgentStream(t, deviceID)
	performClientHandshake(t, stream, agentCertDER)
	sendAgentRegister(t, stream)
	drainRegisterHardwareRequest(t, stream)
	return stream
}

func (e *agentTestEnv) connectAgent(t *testing.T, siteID uuid.UUID) (*quic.Stream, uuid.UUID) {
	t.Helper()
	deviceID := uuid.New()
	e.seedDevice(t, deviceID, siteID)
	stream, agentCertDER := e.dialAgentStream(t, deviceID)
	performClientHandshake(t, stream, agentCertDER)
	sendAgentRegister(t, stream)
	drainRegisterHardwareRequest(t, stream)
	return stream, deviceID
}

func (e *agentTestEnv) connectAgentFastPath(t *testing.T, siteID uuid.UUID, cachedCAHash [48]byte) (*quic.Stream, uuid.UUID) {
	t.Helper()
	deviceID := uuid.New()
	e.seedDevice(t, deviceID, siteID)
	stream, _ := e.dialAgentStream(t, deviceID)
	_, err := stream.Write(protocol.EncodeSkipAuth(cachedCAHash))
	require.NoError(t, err)
	return stream, deviceID
}

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

	codec := &protocol.Codec{}
	hbMsg := &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	}
	payload, err := codec.EncodeControl(hbMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))

	require.Eventually(t, func() bool {
		d, err := env.devices.Get(defaultTenantContext(), deviceID)
		return err == nil && !d.UpdatedAt.Before(originalLastSeen)
	}, 2*time.Second, 50*time.Millisecond)
	assert.Equal(t, db.StatusOnline, getDevice(t, env, deviceID).Status)
}

func TestAgentConnect_DisconnectSetsOffline(t *testing.T) {
	t.Parallel()
	env, stream, deviceID := setupOnlineAgent(t)

	stream.Close()

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOffline)
}

func TestAgentConnect_FastPath_ValidHashRegisters(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	site := testutil.SeedSite(t, ctx, env.store)

	stream, deviceID := env.connectAgentFastPath(t, site.ID, env.caCertHash())
	sendAgentRegister(t, stream)

	waitForDeviceStatus(t, env.store, deviceID, db.StatusOnline)
}

func TestAgentConnect_FastPath_StaleHashRejected(t *testing.T) {
	t.Parallel()
	env := newAgentTestEnv(t)
	ctx := context.Background()
	site := testutil.SeedSite(t, ctx, env.store)

	var staleHash [48]byte
	stream, deviceID := env.connectAgentFastPath(t, site.ID, staleHash)

	require.NoError(t, stream.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, err := stream.Read(make([]byte, 1))
	require.Error(t, err, "server must reject a stale fast-path hash")

	assert.Equal(t, db.StatusOffline, getDevice(t, env, deviceID).Status)
}
