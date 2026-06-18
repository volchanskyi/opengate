package agentapi

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/cert"
)

// W3 spike — QUIC 0-RTT / TLS 1.3 session resumption with mTLS.
//
// These tests are the empirical artifact behind the W3 decision (see the
// archived fast-path-w3-0rtt-eval plan). They prove, against the repo's own
// quic-go v0.60.0 + mutual-TLS cert config, that:
//
//   - 1-RTT session resumption completes with RequireAndVerifyClientCert and
//     preserves the client's verified identity server-side (DidResume == true,
//     PeerCertificates still populated) — a reconnect skips the full asymmetric
//     handshake without weakening mTLS.
//   - The per-reconnect saving is measured by BenchmarkQUICHandshake_{Cold,Resumed}.
//   - 0-RTT early-data behaviour with client certs is whatever quic-go actually
//     does here — asserted so the replay-safety analysis stays anchored to
//     observed behaviour, not assumption.

const resumeTestALPN = "opengate"

// signalingCache wraps an LRU client-session cache and signals every Put so a
// warm-up dial can deterministically wait for the session ticket to be cached
// before the resuming dial runs (TLS 1.3 tickets arrive after the handshake).
type signalingCache struct {
	inner tls.ClientSessionCache
	put   chan struct{}
}

func newSignalingCache() *signalingCache {
	return &signalingCache{
		inner: tls.NewLRUClientSessionCache(8),
		put:   make(chan struct{}, 8),
	}
}

func (c *signalingCache) Get(key string) (*tls.ClientSessionState, bool) {
	return c.inner.Get(key)
}

func (c *signalingCache) Put(key string, cs *tls.ClientSessionState) {
	c.inner.Put(key, cs)
	select {
	case c.put <- struct{}{}:
	default:
	}
}

// resumeTestServer is a minimal QUIC echo server used by the resumption spike.
// It records the TLS connection state of the most recent accepted connection so
// the test can assert DidResume / PeerCertificates from the server's view.
type resumeTestServer struct {
	addr string

	mu        sync.Mutex
	lastState tls.ConnectionState
	lastUsed0 bool
}

func (s *resumeTestServer) snapshot() (tls.ConnectionState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastState, s.lastUsed0
}

// startResumeTestServer brings up a localhost QUIC listener with the repo's mTLS
// server config. allow0RTT toggles server-side early-data acceptance.
func startResumeTestServer(tb testing.TB, mgr *cert.Manager, allow0RTT bool) *resumeTestServer {
	tb.Helper()

	tlsCfg, err := mgr.ServerTLSConfig()
	if err != nil {
		tb.Fatalf("server TLS config: %v", err)
	}
	udpConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		tb.Fatalf("listen udp: %v", err)
	}

	tr := &quic.Transport{Conn: udpConn}
	ln, err := tr.Listen(tlsCfg, &quic.Config{
		MaxIdleTimeout:  30 * time.Second,
		KeepAlivePeriod: 10 * time.Second,
		Allow0RTT:       allow0RTT,
	})
	if err != nil {
		tb.Fatalf("quic listen: %v", err)
	}

	srv := &resumeTestServer{addr: ln.Addr().String()}
	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(func() {
		cancel()
		_ = ln.Close()
		_ = tr.Close()
		_ = udpConn.Close()
	})

	go srv.acceptLoop(ctx, ln)
	return srv
}

func (s *resumeTestServer) acceptLoop(ctx context.Context, ln *quic.Listener) {
	for {
		conn, err := ln.Accept(ctx)
		if err != nil {
			return
		}
		go s.serve(ctx, conn)
	}
}

// serve accepts one bidirectional stream, echoes a single byte, and records the
// connection's TLS state. The byte round-trip drives the handshake (and the
// post-handshake session ticket) to completion.
func (s *resumeTestServer) serve(ctx context.Context, conn *quic.Conn) {
	st := conn.ConnectionState()
	s.mu.Lock()
	s.lastState, s.lastUsed0 = st.TLS, st.Used0RTT
	s.mu.Unlock()

	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return
	}
	buf := make([]byte, 1)
	if _, err := stream.Read(buf); err != nil && !errors.Is(err, context.Canceled) {
		return
	}
	_, _ = stream.Write(buf)
	_ = stream.Close()

	// Hold the connection open briefly so the session ticket reaches the client.
	select {
	case <-ctx.Done():
	case <-time.After(200 * time.Millisecond):
	}
	_ = conn.CloseWithError(0, "bye")
}

// agentResumeTLSConfig builds an agent (client) mTLS config wired with the given
// session cache — the cache is what production quinn would need to persist.
func agentResumeTLSConfig(tb testing.TB, mgr *cert.Manager, cache tls.ClientSessionCache) *tls.Config {
	tb.Helper()
	agentCert, err := mgr.SignAgent(uuid.NewString(), "resume-test-host")
	if err != nil {
		tb.Fatalf("sign agent: %v", err)
	}
	cfg := mgr.AgentTLSConfig(agentCert)
	cfg.ServerName = "localhost"
	cfg.ClientSessionCache = cache
	cfg.NextProtos = []string{resumeTestALPN}
	return cfg
}

// streamPing opens a bidi stream, sends one byte, and reads the echo. A QUIC
// Read can return the final byte together with io.EOF (peer FIN in the same
// flight); that is success, not failure.
func streamPing(tb testing.TB, ctx context.Context, conn *quic.Conn) {
	tb.Helper()
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		tb.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Write([]byte{0x14}); err != nil {
		tb.Fatalf("write: %v", err)
	}
	buf := make([]byte, 1)
	if n, err := stream.Read(buf); n == 0 && err != nil {
		tb.Fatalf("read: %v", err)
	}
	_ = stream.Close()
}

// waitForTicket blocks until the server has issued a TLS session ticket into the
// cache, so a following dial can deterministically resume.
func waitForTicket(tb testing.TB, cache *signalingCache) {
	tb.Helper()
	select {
	case <-cache.put:
	case <-time.After(5 * time.Second):
		tb.Fatalf("server never issued a TLS session ticket")
	}
}

// dialRoundTrip performs a full QUIC dial + stream ping and returns the client's
// TLS state.
func dialRoundTrip(tb testing.TB, addr string, tlsCfg *tls.Config) tls.ConnectionState {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := quic.DialAddr(ctx, addr, tlsCfg, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	if err != nil {
		tb.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.CloseWithError(0, "done") }()

	streamPing(tb, ctx, conn)
	return conn.ConnectionState().TLS
}

func TestQUICColdHandshake_FullMTLS(t *testing.T) {
	t.Parallel()
	mgr, err := cert.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	srv := startResumeTestServer(t, mgr, false)

	// No session cache -> every dial is a full mTLS handshake.
	if clientState := dialRoundTrip(t, srv.addr, agentResumeTLSConfig(t, mgr, nil)); clientState.DidResume {
		t.Fatalf("cold handshake unexpectedly resumed")
	}

	serverState, _ := srv.snapshot()
	if serverState.DidResume {
		t.Fatalf("server saw a resumed session on the cold path")
	}
	if len(serverState.PeerCertificates) == 0 {
		t.Fatalf("server did not capture the client certificate (mTLS not enforced)")
	}
}

func TestQUICSessionResumption_PreservesMTLSIdentity(t *testing.T) {
	t.Parallel()
	mgr, err := cert.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	srv := startResumeTestServer(t, mgr, false)

	cache := newSignalingCache()
	clientCfg := agentResumeTLSConfig(t, mgr, cache)

	// Warm-up dial primes the ticket; the resuming dial must then resume AND
	// still present the verified client identity.
	if warm := dialRoundTrip(t, srv.addr, clientCfg); warm.DidResume {
		t.Fatalf("warm-up dial unexpectedly resumed")
	}
	waitForTicket(t, cache)

	if resumed := dialRoundTrip(t, srv.addr, clientCfg); !resumed.DidResume {
		t.Fatalf("reconnect did not resume the TLS session (no per-reconnect saving)")
	}

	serverState, _ := srv.snapshot()
	if !serverState.DidResume {
		t.Fatalf("server did not treat the reconnect as resumed")
	}
	if len(serverState.PeerCertificates) == 0 {
		t.Fatalf("resumed reconnect lost the client certificate identity — mTLS regressed")
	}
	if serverState.PeerCertificates[0].Subject.CommonName == "" {
		t.Fatalf("resumed client certificate has no CommonName (identity not carried)")
	}
}

func TestQUIC0RTT_ClientCertBehaviour(t *testing.T) {
	t.Parallel()
	mgr, err := cert.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	srv := startResumeTestServer(t, mgr, true)

	cache := newSignalingCache()
	clientCfg := agentResumeTLSConfig(t, mgr, cache)

	// Warm up to obtain a ticket (which carries the 0-RTT allowance, if any).
	_ = dialRoundTrip(t, srv.addr, clientCfg)
	waitForTicket(t, cache)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// DialAddrEarly sends 0-RTT early data before the handshake completes;
	// whether the server accepts it with client certs is what we measure.
	conn, err := quic.DialAddrEarly(ctx, srv.addr, clientCfg, &quic.Config{MaxIdleTimeout: 30 * time.Second})
	if err != nil {
		t.Fatalf("dial early: %v", err)
	}
	defer func() { _ = conn.CloseWithError(0, "done") }()

	streamPing(t, ctx, conn)
	select {
	case <-conn.HandshakeComplete():
	case <-time.After(5 * time.Second):
		t.Fatalf("handshake never completed")
	}

	t.Logf("W3 0-RTT spike: client Used0RTT=%v (mTLS, quic-go v0.60.0)", conn.ConnectionState().Used0RTT)

	// Security invariant regardless of 0-RTT: the server must still hold the
	// verified client identity — 0-RTT must never silently drop mTLS.
	serverState, serverUsed0 := srv.snapshot()
	t.Logf("W3 0-RTT spike: server Used0RTT=%v DidResume=%v", serverUsed0, serverState.DidResume)
	if len(serverState.PeerCertificates) == 0 {
		t.Fatalf("0-RTT/resumed connection lost client certificate — mTLS regressed")
	}
}
