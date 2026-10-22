package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

type acceptEnv struct {
	srv     *AgentServer
	addr    string
	devices device.Repository
	issuer  *devicecert.Issuer
	cancel  context.CancelFunc
}

func newAcceptEnv(t *testing.T) *acceptEnv {
	t.Helper()
	return newAcceptEnvWithMetrics(t, nil)
}

func newAcceptEnvWithMetrics(t *testing.T, m *appmetrics.Metrics) *acceptEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	devices := testutil.NewTestDevices(t, store)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	issuer := machinecert.NewIssuer(t, store, cm, devices)
	srv := NewAgentServer(AgentServerConfig{
		Issuer:        issuer,
		Cert:          cm,
		Devices:       devices,
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(testLogger()),
		Notifier:      &notifications.NoopNotifier{},
		Metrics:       m,
		Logger:        testLogger(),
	})

	ctx, cancel := context.WithCancel(context.Background())
	listening := make(chan struct{})
	go func() {
		defer close(listening)
		_ = srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	addr := srv.Addr()

	t.Cleanup(func() {
		cancel()
		select {
		case <-listening:
		case <-time.After(5 * time.Second):
			t.Error("the QUIC listener did not stop when its context was cancelled")
		}
	})
	return &acceptEnv{srv: srv, addr: addr, devices: devices, issuer: issuer, cancel: cancel}
}

// machineTLS is a recorded certificate for deviceID and the configuration to dial with it.
func (e *acceptEnv) machineTLS(t *testing.T, deviceID uuid.UUID) *tls.Config {
	t.Helper()
	return clientTLS(e.srv.cert, machinecert.Issue(t, e.issuer, deviceID))
}

// dial sends the AgentHello and stops; a deleted machine is closed right after the handshake,
// so a read after dial races that close.
func (e *acceptEnv) dial(t *testing.T, deviceID uuid.UUID) (*quic.Conn, *quic.Stream) {
	t.Helper()
	return e.dialWith(t, e.machineTLS(t, deviceID))
}

// dialWith dials over a caller-kept config so one certificate and session cache span
// attempts, which makes a reconnect resumable.
func (e *acceptEnv) dialWith(t *testing.T, tlsCfg *tls.Config) (*quic.Conn, *quic.Stream) {
	t.Helper()
	require.NotEmpty(t, tlsCfg.Certificates,
		"a machine reaches this listener holding a signed agent certificate")

	conn, err := quic.DialAddr(t.Context(), e.addr,
		tlsCfg, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })

	// The machine opens the stream and writes first, so the stream id is even.
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	require.Zero(t, int64(stream.StreamID())%2, "the control stream is the machine's")

	certHash := sha512.Sum384(tlsCfg.Certificates[0].Certificate[0])
	var nonce [32]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = stream.Write(protocol.EncodeAgentHello(nonce, certHash))
	require.NoError(t, err)
	return conn, stream
}

func (e *acceptEnv) connect(t *testing.T, deviceID uuid.UUID) (*quic.Conn, *quic.Stream) {
	t.Helper()
	conn, stream := e.dial(t, deviceID)
	readServerHello(t, stream)
	return conn, stream
}

func readServerHello(t *testing.T, stream *quic.Stream) {
	t.Helper()
	serverHello := make([]byte, 81)
	_, err := io.ReadFull(stream, serverHello)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), serverHello[0])
}

func register(t *testing.T, stream *quic.Stream, hostname string) {
	t.Helper()
	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
		Hostname:     hostname,
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, payload))
}

func waitForCount(t *testing.T, srv *AgentServer, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if srv.ConnectedAgentCount() == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("connected agents never reached %d (holding %d)", want, srv.ConnectedAgentCount())
}

// waitForStatus polls the machine's row; the connection count moves on the accept path,
// before the register frame is handled.
func waitForStatus(t *testing.T, env *acceptEnv, deviceID uuid.UUID, want device.DeviceStatus) {
	t.Helper()
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	deadline := time.Now().Add(10 * time.Second)
	var last device.DeviceStatus
	for time.Now().Before(deadline) {
		stored, err := env.devices.Get(ctx, deviceID)
		if err == nil {
			if stored.Status == want {
				return
			}
			last = stored.Status
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the machine's status never reached %q (holding %q)", want, last)
}

func TestAMachineThatConnectsBecomesVisibleAndThenOffline(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := uuid.New()
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	// The seeded hostname differs from the registered one so the assertion reflects registration.
	require.NoError(t, env.devices.Upsert(ctx, &device.Device{
		ID:       deviceID,
		Hostname: "as-enrolled",
		OS:       "linux",
		Status:   db.StatusOffline,
	}))

	_, stream := env.connect(t, deviceID)
	register(t, stream, "the-machine")

	waitForCount(t, env.srv, 1)
	ac := env.srv.GetAgent(protocol.DeviceID(deviceID))
	require.NotNil(t, ac, "the machine that registered is the one the fleet holds")
	assert.Equal(t, deviceID, uuid.UUID(ac.DeviceID))

	// The count moves on the accept path before registration lands, so the row is awaited.
	waitForStatus(t, env, deviceID, db.StatusOnline)
	stored, err := env.devices.Get(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "the-machine", stored.Hostname)

	require.NoError(t, stream.Close())
	waitForCount(t, env.srv, 0)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d, err := env.devices.Get(ctx, deviceID); err == nil && d.Status == db.StatusOffline {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a machine that left was never marked offline")
}

func TestAMachineTheDatabaseHasNotSeenStillRegisters(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := uuid.New()

	_, stream := env.connect(t, deviceID)
	register(t, stream, "brand-new")

	waitForCount(t, env.srv, 1)
	ac := env.srv.GetAgent(protocol.DeviceID(deviceID))
	require.NotNil(t, ac)
	assert.Equal(t, uuid.Nil, ac.SiteID, "a machine nobody has filed yet is in no site")
}
