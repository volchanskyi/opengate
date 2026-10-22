package agentapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

// serverClock is the clock the server decides certificate questions by, moved by hand.
type serverClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *serverClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *serverClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// certEnv is a listening agent server whose certificate decisions read a hand-moved clock.
type certEnv struct {
	clock   *serverClock
	store   *db.PostgresStore
	cm      *cert.Manager
	issuer  *devicecert.Issuer
	srv     *AgentServer
	devices device.Repository
	addr    string
}

func newCertEnv(t *testing.T, dir string, opts ...cert.Option) *certEnv {
	t.Helper()
	return newCertEnvLogging(t, dir, testLogger(), opts...)
}

// newCertEnvLogging is newCertEnv whose server logs to logger.
func newCertEnvLogging(t *testing.T, dir string, logger *slog.Logger, opts ...cert.Option) *certEnv {
	t.Helper()
	clock := &serverClock{t: time.Now()}
	if dir == "" {
		dir = t.TempDir()
	}
	cm, err := cert.NewManager(dir, append([]cert.Option{cert.WithClock(clock.Now)}, opts...)...)
	require.NoError(t, err)
	store := testutil.NewTestStore(t)
	devices := testutil.NewTestDevices(t, store)
	issuer := machinecert.NewIssuer(t, store, cm, devices)
	srv := NewAgentServer(AgentServerConfig{
		Cert:          cm,
		Issuer:        issuer,
		Devices:       devices,
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(testLogger()),
		Notifier:      &notifications.NoopNotifier{},
		Logger:        logger,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()
	addr := srv.Addr()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return &certEnv{clock: clock, store: store, cm: cm, issuer: issuer, srv: srv, devices: devices, addr: addr}
}

// machine is a recorded certificate for a new device number, with its device row filed.
func (e *certEnv) machine(t *testing.T) (uuid.UUID, *tls.Certificate) {
	t.Helper()
	id := uuid.New()
	site := testutil.SeedSite(t, context.Background(), e.store)
	require.NoError(t, e.devices.Upsert(dbtx.WithDefaultTenant(context.Background(), false), &device.Device{
		ID: id, SiteID: site.ID, Hostname: "machine", OS: "linux", Status: device.StatusOffline,
	}))
	return id, machinecert.Issue(t, e.issuer, id)
}

// tlsFor is the machine's TLS configuration, on the server's clock so a far-future server
// certificate still verifies.
func (e *certEnv) tlsFor(machine *tls.Certificate) *tls.Config {
	cfg := clientTLS(e.cm, machine)
	cfg.Time = e.clock.Now
	return cfg
}

// machineConn is one connection a test machine holds.
type machineConn struct {
	conn   *quic.Conn
	stream *quic.Stream
	codec  *protocol.Codec
	inbox  chan *protocol.ControlMessage
	hello  [81]byte
}

// dial connects with a full handshake and starts reading control frames, answering pings.
func (e *certEnv) dial(t *testing.T, machine *tls.Certificate) *machineConn {
	t.Helper()
	conn, err := quic.DialAddr(t.Context(), e.addr, e.tlsFor(machine), &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	var nonce [32]byte
	_, _ = rand.Read(nonce[:])
	_, err = stream.Write(protocol.EncodeAgentHello(nonce, sha512.Sum384(machine.Certificate[0])))
	require.NoError(t, err)
	// A refused machine may be closed before it reads the hello, so the hello is optional here.
	m := &machineConn{conn: conn, stream: stream, codec: &protocol.Codec{}, inbox: make(chan *protocol.ControlMessage, 64)}
	if _, err := io.ReadFull(stream, m.hello[:]); err != nil {
		close(m.inbox)
		return m
	}
	go m.readLoop()
	return m
}

// dialFast reconnects on the fast path with the given cached master-key fingerprint.
func (e *certEnv) dialFast(t *testing.T, machine *tls.Certificate, cached [48]byte) *machineConn {
	t.Helper()
	conn, err := quic.DialAddr(t.Context(), e.addr, e.tlsFor(machine), &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	stream, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	_, err = stream.Write(protocol.EncodeSkipAuth(cached))
	require.NoError(t, err)
	m := &machineConn{conn: conn, stream: stream, codec: &protocol.Codec{}, inbox: make(chan *protocol.ControlMessage, 64)}
	go m.readLoop()
	return m
}

func (m *machineConn) readLoop() {
	defer close(m.inbox)
	for {
		frameType, payload, err := m.codec.ReadFrame(m.stream)
		if err != nil {
			return
		}
		switch frameType {
		case protocol.FramePing:
			_ = m.codec.WriteFrame(m.stream, protocol.FramePong, nil)
		case protocol.FrameControl:
			if msg, err := m.codec.DecodeControl(payload); err == nil {
				m.inbox <- msg
			}
		}
	}
}

func (m *machineConn) send(t *testing.T, msg *protocol.ControlMessage) {
	t.Helper()
	payload, err := m.codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, m.codec.WriteFrame(m.stream, protocol.FrameControl, payload))
}

func (m *machineConn) register(t *testing.T) {
	t.Helper()
	m.send(t, &protocol.ControlMessage{
		Type: protocol.MsgAgentRegister, Hostname: "machine", OS: "linux", Arch: "amd64", Version: "1.0.0",
	})
}

// await returns the first message of the given type, failing after the wait.
func (m *machineConn) await(t *testing.T, want protocol.ControlMessageType, wait time.Duration) *protocol.ControlMessage {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case msg, ok := <-m.inbox:
			if !ok {
				t.Fatalf("the connection closed before a %s arrived: %v", want, context.Cause(m.conn.Context()))
			}
			if msg.Type == want {
				return msg
			}
		case <-timer.C:
			t.Fatalf("no %s within %s", want, wait)
		}
	}
}

// closedWith waits for the server to close the connection and returns the code it used.
func (m *machineConn) closedWith(t *testing.T, wait time.Duration) quic.ApplicationErrorCode {
	t.Helper()
	select {
	case <-m.conn.Context().Done():
	case <-time.After(wait):
		t.Fatalf("the server held the connection open past %s", wait)
	}
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(m.conn.Context()), &appErr)
	return appErr.ErrorCode
}

// renewalRequest is a P-256 request naming cn, and its key.
func renewalRequest(t *testing.T, cn string) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.CertificateRequest{}
	tmpl.Subject.CommonName = cn
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	require.NoError(t, err)
	return der, key
}

// logBuffer collects what a server logs, read safely while the server writes.
type logBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
