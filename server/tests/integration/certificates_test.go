package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"sync"
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
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

const day = 24 * time.Hour

// clock is the time the server decides certificate questions by, moved by hand.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// certServer is one running server over a store and a trust set, standing in for one release.
type certServer struct {
	cm     *cert.Manager
	issuer *devicecert.Issuer
	srv    *agentapi.AgentServer
	addr   string
	clock  *clock
}

func startCertServer(t *testing.T, store *db.PostgresStore, dir string, c *clock, opts ...cert.Option) *certServer {
	t.Helper()
	cm, err := cert.NewManager(dir, append([]cert.Option{cert.WithClock(c.Now)}, opts...)...)
	require.NoError(t, err)
	devices := testutil.NewTestDevices(t, store)
	issuer := machinecert.NewIssuer(t, store, cm, devices)
	require.NoError(t, issuer.Ledger().Warm(context.Background()))
	srv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          cm,
		Issuer:        issuer,
		Devices:       devices,
		Hardware:      testutil.NewTestHardware(t, store),
		DeviceUpdates: testutil.NewTestDeviceUpdates(t, store),
		Relay:         relay.NewRelay(testLogger()),
		Notifier:      &notifications.NoopNotifier{},
		Logger:        testLogger(),
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
	return &certServer{cm: cm, issuer: issuer, srv: srv, addr: addr, clock: c}
}

// session is one connection a machine holds, with a reader collecting control messages.
type session struct {
	conn  *quic.Conn
	strm  *quic.Stream
	codec *protocol.Codec
	inbox chan *protocol.ControlMessage
}

func (s *certServer) connect(t *testing.T, machine *tls.Certificate, fastPath *[48]byte) *session {
	t.Helper()
	cfg := machinecert.ClientTLS(s.cm, machine)
	cfg.Time = s.clock.Now
	conn, err := quic.DialAddr(t.Context(), s.addr, cfg, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.CloseWithError(0, "test done") })
	strm, err := conn.OpenStreamSync(t.Context())
	require.NoError(t, err)
	if fastPath != nil {
		_, err = strm.Write(protocol.EncodeSkipAuth(*fastPath))
		require.NoError(t, err)
	} else {
		performClientHandshakeTolerant(t, strm, machine.Certificate[0])
	}
	m := &session{conn: conn, strm: strm, codec: &protocol.Codec{}, inbox: make(chan *protocol.ControlMessage, 64)}
	go func() {
		defer close(m.inbox)
		for {
			frameType, payload, err := m.codec.ReadFrame(strm)
			if err != nil {
				return
			}
			if frameType == protocol.FrameControl {
				if msg, err := m.codec.DecodeControl(payload); err == nil {
					m.inbox <- msg
				}
			}
		}
	}()
	return m
}

// performClientHandshakeTolerant writes AgentHello and reads the reply, which a server refusing
// the machine may close before it arrives.
func performClientHandshakeTolerant(t *testing.T, strm *quic.Stream, certDER []byte) {
	t.Helper()
	var nonce [32]byte
	_, err := rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = strm.Write(protocol.EncodeAgentHello(nonce, sha512.Sum384(certDER)))
	require.NoError(t, err)
	_, _ = io.ReadFull(strm, make([]byte, 81))
}

func (m *session) send(t *testing.T, msg *protocol.ControlMessage) {
	t.Helper()
	payload, err := m.codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, m.codec.WriteFrame(m.strm, protocol.FrameControl, payload))
}

func (m *session) register(t *testing.T) {
	t.Helper()
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgAgentRegister, Hostname: "returning", OS: "linux"})
}

func (m *session) await(t *testing.T, want protocol.ControlMessageType) *protocol.ControlMessage {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg, ok := <-m.inbox:
			require.True(t, ok, "the connection closed before a %s: %v", want, context.Cause(m.conn.Context()))
			if msg.Type == want {
				return msg
			}
		case <-deadline:
			t.Fatalf("no %s arrived", want)
		}
	}
}

func (m *session) closedWith(t *testing.T) quic.ApplicationErrorCode {
	t.Helper()
	select {
	case <-m.conn.Context().Done():
	case <-time.After(15 * time.Second):
		t.Fatal("the server held the connection open")
	}
	var appErr *quic.ApplicationError
	require.ErrorAs(t, context.Cause(m.conn.Context()), &appErr)
	return appErr.ErrorCode
}

// renew answers the server's offer with a request from a fresh key, installs what is issued, and
// returns the new certificate.
func (m *session) renew(t *testing.T, id uuid.UUID) *tls.Certificate {
	t.Helper()
	m.await(t, protocol.MsgRenewCertificate)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: id.String()},
	}, key)
	require.NoError(t, err)
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateSigningRequest, CSR: csr})
	issued := m.await(t, protocol.MsgCertificateIssued)
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})
	return &tls.Certificate{Certificate: [][]byte{issued.Cert}, PrivateKey: key}
}

// enrolled issues a certificate for a new device whose row a first registration has filed.
func enrolled(t *testing.T, s *certServer, store *db.PostgresStore) (uuid.UUID, *tls.Certificate) {
	t.Helper()
	id := uuid.New()
	site := testutil.SeedSite(t, context.Background(), store)
	require.NoError(t, testutil.NewTestDevices(t, store).Upsert(defaultTenantContext(), &device.Device{
		ID: id, SiteID: site.ID, Hostname: "returning", OS: "linux", Status: device.StatusOffline,
	}))
	return id, machinecert.Issue(t, s.issuer, id)
}

func TestATombstonedMachineOnTheFastPathReceivesItsRemovalOrder(t *testing.T) {
	store := testutil.NewTestStore(t)
	s := startCertServer(t, store, t.TempDir(), &clock{t: time.Now()})
	id := uuid.New()
	machine := machinecert.Issue(t, s.issuer, id)
	s.srv.DeregisterAgent(context.Background(), id)

	issuer := s.cm.IssuerHash()
	m := s.connect(t, machine, &issuer)
	m.register(t)
	order := m.await(t, protocol.MsgAgentDeregistered)
	assert.Equal(t, "device deleted by administrator", order.Reason,
		"the order arrives before the connection closes")
}

func TestAMachineRenewsOverItsLiveConnection(t *testing.T) {
	store := testutil.NewTestStore(t)
	c := &clock{t: time.Now()}
	s := startCertServer(t, store, t.TempDir(), c)
	id := uuid.New()
	machine := machinecert.Issue(t, s.issuer, id)
	c.Advance(61 * day)

	m := s.connect(t, machine, nil)
	m.register(t)
	m.await(t, protocol.MsgTrustBundle)
	renewed := m.renew(t, id)

	again := s.connect(t, renewed, nil)
	again.register(t)
	again.await(t, protocol.MsgTrustBundle)
	stale := s.connect(t, machine, nil)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), stale.closedWith(t))
}

func TestAnExpiredMachineIsAdmittedOnlyToRenew(t *testing.T) {
	store := testutil.NewTestStore(t)
	c := &clock{t: time.Now()}
	s := startCertServer(t, store, t.TempDir(), c)
	id, machine := enrolled(t, s, store)
	c.Advance(100 * day)

	m := s.connect(t, machine, nil)
	m.register(t)
	assert.Equal(t, 0, s.srv.ConnectedAgentCount(), "an expired machine is never online")
	renewed := m.renew(t, id)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseRestrictedSessionEnded), m.closedWith(t))

	full := s.connect(t, renewed, nil)
	full.register(t)
	full.await(t, protocol.MsgTrustBundle)
	assert.Equal(t, 1, s.srv.ConnectedAgentCount())
}
