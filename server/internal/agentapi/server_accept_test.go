package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
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
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The connection lifecycle, driven end to end in this process.
//
// A machine reaching this server crosses four doors in order — the control
// stream it opens, the handshake that names it, the registration that makes it
// visible, and the teardown that marks it offline — and each of them lives in
// this package. Until this file existed nothing here drove any of them: the
// only tests that did lived in the integration tier, whose coverage is measured
// against nothing, so the whole accept path read as untested and was carved out
// of the coverage gate rather than covered.
//
// The listener is a real QUIC transport on loopback with the product's own mTLS
// configuration, so what is asserted is what a machine actually gets.

// acceptEnv is a listening AgentServer plus what a test needs to reach it.
type acceptEnv struct {
	srv     *AgentServer
	addr    string
	devices device.Repository
	cancel  context.CancelFunc
}

// newAcceptEnv starts the product's QUIC listener on loopback and returns once
// it is accepting. The listener stops when the test ends.
func newAcceptEnv(t *testing.T) *acceptEnv {
	t.Helper()
	store := testutil.NewTestStore(t)
	devices := testutil.NewTestDevices(t, store)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	srv := NewAgentServer(AgentServerConfig{
		Cert:          cm,
		Devices:       devices,
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(testLogger()),
		Notifier:      &notifications.NoopNotifier{},
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
	return &acceptEnv{srv: srv, addr: addr, devices: devices, cancel: cancel}
}

// dial reaches the listener as a machine holding a signed agent certificate,
// opens the control stream and greets the server. It stops there: a machine the
// administrator deleted is closed at the next door, so anything read after this
// races that close.
func (e *acceptEnv) dial(t *testing.T, deviceID uuid.UUID) (*quic.Conn, *quic.Stream) {
	t.Helper()
	tlsCert, err := e.srv.cert.SignAgent(deviceID.String(), "accept-test")
	require.NoError(t, err)

	conn, err := quic.DialAddr(t.Context(), e.addr,
		e.srv.cert.AgentTLSConfig(tlsCert), &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })

	// The machine opens the stream and writes first, so the stream id is even.
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	require.Zero(t, int64(stream.StreamID())%2, "the control stream is the machine's")

	certHash := sha512.Sum384(tlsCert.Certificate[0])
	var nonce [32]byte
	_, err = rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = stream.Write(protocol.EncodeAgentHello(nonce, certHash))
	require.NoError(t, err)
	return conn, stream
}

// connect dials and reads back the server's greeting, so the caller holds a
// stream the handshake has already completed on.
func (e *acceptEnv) connect(t *testing.T, deviceID uuid.UUID) (*quic.Conn, *quic.Stream) {
	t.Helper()
	conn, stream := e.dial(t, deviceID)
	serverHello := make([]byte, 81)
	_, err := io.ReadFull(stream, serverHello)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), serverHello[0])
	return conn, stream
}

// register sends the frame that makes a machine visible to the fleet.
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

// waitForCount polls until the server holds want connections, so an assertion
// is about the outcome rather than about how quickly a goroutine got there.
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

// TestAMachineThatConnectsBecomesVisibleAndThenOffline walks the whole
// lifecycle: the machine dials, hands over its certificate, registers, is
// counted, and — when it goes away — is marked offline and stops being counted.
func TestAMachineThatConnectsBecomesVisibleAndThenOffline(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := uuid.New()
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	require.NoError(t, env.devices.Upsert(ctx, &device.Device{
		ID:       deviceID,
		Hostname: "the-machine",
		OS:       "linux",
		Status:   db.StatusOffline,
	}))

	_, stream := env.connect(t, deviceID)
	register(t, stream, "the-machine")

	waitForCount(t, env.srv, 1)
	ac := env.srv.GetAgent(protocol.DeviceID(deviceID))
	require.NotNil(t, ac, "the machine that registered is the one the fleet holds")
	assert.Equal(t, deviceID, uuid.UUID(ac.DeviceID))

	stored, err := env.devices.Get(ctx, deviceID)
	require.NoError(t, err)
	assert.Equal(t, "the-machine", stored.Hostname)

	// The machine leaves.
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

// TestAMachineTheAdministratorDeletedIsTurnedAway covers the door that runs
// between the handshake and registration: a device the administrator removed is
// closed with the deregistration code, and never becomes a connection the fleet
// holds.
func TestAMachineTheAdministratorDeletedIsTurnedAway(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := uuid.New()
	env.srv.tombstones.Store(deviceID, time.Now())

	conn, _ := env.dial(t, deviceID)

	select {
	case <-conn.Context().Done():
	case <-time.After(10 * time.Second):
		t.Fatal("a deleted machine was left holding an open connection")
	}
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(conn.Context()), &appErr,
		"the server closes the connection itself rather than letting it time out")
	assert.Equal(t, quic.ApplicationErrorCode(3), appErr.ErrorCode)
	assert.Contains(t, appErr.ErrorMessage, "deregistered",
		"and says why, so the machine stops trying")

	assert.Equal(t, 0, env.srv.ConnectedAgentCount(),
		"a machine turned away at the door never joins the fleet")
}

// TestAMachineTheDatabaseHasNotSeenStillRegisters covers the fallbacks either
// side of registration: a machine connecting before any row exists for it has
// no tenant to resolve and no hostname to look up, and neither is a reason to
// turn it away — a first enrolment is exactly this shape.
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

// TestASecondConnectionLeavesTheLiveOneAlone is the property a reconnect
// depends on. A machine that drops and comes straight back leaves two teardowns
// racing one registration, and the older one must not mark a machine offline
// that is connected right now — an operator would see a live machine reported
// as gone.
func TestASecondConnectionLeavesTheLiveOneAlone(t *testing.T) {
	env := newAcceptEnv(t)
	deviceID := uuid.New()
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	require.NoError(t, env.devices.Upsert(ctx, &device.Device{
		ID:       deviceID,
		Hostname: "the-machine",
		OS:       "linux",
		Status:   db.StatusOffline,
	}))

	_, first := env.connect(t, deviceID)
	register(t, first, "the-machine")
	waitForCount(t, env.srv, 1)
	firstConn := env.srv.GetAgent(protocol.DeviceID(deviceID))
	require.NotNil(t, firstConn)

	// The machine comes back before the old connection has been torn down.
	_, second := env.connect(t, deviceID)
	register(t, second, "the-machine")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if env.srv.GetAgent(protocol.DeviceID(deviceID)) != firstConn {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.NotSame(t, firstConn, env.srv.GetAgent(protocol.DeviceID(deviceID)),
		"the newer connection is the one the fleet holds")

	// One machine is connected, however many times it dialled to get there.
	// The count is what the platform's connected-agents gauge reads, so a
	// second connection that added one the teardown of the first cannot take
	// back would raise the fleet's size for the life of the process.
	assert.Equal(t, 1, env.srv.ConnectedAgentCount(),
		"two connections to one machine is still one machine")

	// Now the old one finishes leaving. The machine stays online.
	require.NoError(t, first.Close())
	waitForCount(t, env.srv, 1)

	stored, err := env.devices.Get(ctx, deviceID)
	require.NoError(t, err)
	assert.NotEqual(t, db.StatusOffline, stored.Status,
		"a machine that is connected right now is not reported as gone")

	// And when the live one leaves, the fleet is empty again rather than
	// holding the reconnect forever.
	require.NoError(t, second.Close())
	waitForCount(t, env.srv, 0)
}
