package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The tests in this file exercise the APF protocol machinery, so they run
// against in-memory ports. The persistence path — resolving a CIRA connection to
// its device and writing that device's tenant — is covered end to end in
// mps_link_test.go, which drives the real amt.PostgresAMTDevices and
// device.PostgresHardware from an external test package (importing amt from
// package transport would be a build cycle).

// pipeTestTimeout bounds both ends of every in-memory pipe in this package.
const pipeTestTimeout = 5 * time.Second

// newDeadlinePipe returns a net.Pipe whose both ends carry a deadline and are
// closed with the test. net.Pipe is synchronous and unbuffered, so a peer that
// stops reading — or never replies — leaves the other side's Write or Read
// blocked with nothing to interrupt it. Both ends are driven by the test here,
// so both are bounded: a change that breaks the exchange fails in seconds
// instead of hanging until something outside the test gives up on it.
func newDeadlinePipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	deadline := time.Now().Add(pipeTestTimeout)
	require.NoError(t, a.SetDeadline(deadline))
	require.NoError(t, b.SetDeadline(deadline))
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	return a, b
}

// memAMTState records connection-state writes in memory.
type memAMTState struct {
	mu       sync.Mutex
	upserted []db.AMTDevice
	statuses map[uuid.UUID]db.DeviceStatus
}

func newMemAMTState() *memAMTState {
	return &memAMTState{statuses: map[uuid.UUID]db.DeviceStatus{}}
}

func (m *memAMTState) Upsert(_ context.Context, d *db.AMTDevice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.upserted = append(m.upserted, *d)
	m.statuses[d.UUID] = d.Status
	return nil
}

func (m *memAMTState) SetStatus(_ context.Context, id uuid.UUID, status db.DeviceStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[id] = status
	return nil
}

func (m *memAMTState) status(id uuid.UUID) (db.DeviceStatus, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.statuses[id]
	return s, ok
}

// memLinker resolves every system UUID to one fixed device once armed, so the APF
// tests get a linked connection without a database, and the adoption test can
// start unarmed to stand in for a machine whose AMT firmware dialled in before
// its agent had registered.
type memLinker struct {
	mu       sync.Mutex
	deviceID uuid.UUID
	tenantID uuid.UUID
	armed    bool
}

func newMemLinker(armed bool) *memLinker {
	return &memLinker{deviceID: uuid.New(), tenantID: dbtx.DefaultTenantID, armed: armed}
}

func (m *memLinker) ResolveBySystemUUID(_ context.Context, _ uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.armed {
		return uuid.Nil, uuid.Nil, errors.New("no device reports this system uuid")
	}
	return m.deviceID, m.tenantID, nil
}

func (m *memLinker) SetAMTDetail(_ context.Context, _ uuid.UUID, _, _ string) error { return nil }

// arm makes the lookup start succeeding, as a registering agent would.
func (m *memLinker) arm() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.armed = true
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newTestServer builds an MPS server over in-memory ports, returning it with the
// recorded state and the CA PEM a client needs to verify its certificate.
func newTestServer(t *testing.T) (*Server, *memAMTState, []byte) {
	t.Helper()

	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	state := newMemAMTState()
	srv := NewServer(cm, state, newMemLinker(true), discardLogger())
	return srv, state, cm.CACertPEM()
}

func startTestServer(t *testing.T, srv *Server) (string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		_ = srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	addr := srv.Addr()
	t.Cleanup(cancel)
	return addr, cancel
}

// connectAMT dials the MPS listener as an AMT device would. The MPS certificate
// is CA-signed with a localhost SAN, so the client verifies it against the
// server's own CA rather than skipping verification.
func connectAMT(t *testing.T, addr string, caPEM []byte) net.Conn {
	t.Helper()
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(caPEM), "the test CA should be parseable")

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 5 * time.Second},
		"tcp", addr,
		&tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12},
	)
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// simulateCIRA performs the full CIRA handshake from the AMT device side.
func simulateCIRA(t *testing.T, conn net.Conn, amtUUID uuid.UUID) {
	t.Helper()

	// Step 1: Send ProtocolVersion with UUID in Intel mixed-endian format.
	buf := make([]byte, 29)
	buf[0] = APFProtocolVersion
	buf[1] = 0 // major=1 in BE
	buf[2] = 0
	buf[3] = 0
	buf[4] = 1
	// minor=0, trigger=0 (bytes 5..12 are zero)
	// Write UUID in Intel LE format (reverse of ReorderIntelGUID).
	intelBytes := toIntelGUID(amtUUID)
	copy(buf[13:], intelBytes[:])
	_, err := conn.Write(buf)
	require.NoError(t, err)

	// Read server ProtocolVersion response.
	expectMessage(t, conn, APFProtocolVersion)

	// Step 2: Send ServiceRequest (auth).
	require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServiceAuth))
	expectMessage(t, conn, APFServiceAccept)

	// Step 3: Send UserAuthRequest.
	authPayload := encodeAPFString("admin")
	authPayload = append(authPayload, encodeAPFString(ServiceAuth)...)
	authPayload = append(authPayload, encodeAPFString("digest")...)
	msg := append([]byte{APFUserAuthRequest}, authPayload...)
	_, err = conn.Write(msg)
	require.NoError(t, err)
	expectMessage(t, conn, APFUserAuthSuccess)

	// Step 4: Send ServiceRequest (pfwd).
	require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServicePFwd))
	expectMessage(t, conn, APFServiceAccept)

	// Step 5: Send GlobalRequest (tcpip-forward) for port 16992.
	grPayload := encodeAPFString("tcpip-forward")
	grPayload = append(grPayload, 1) // want_reply
	grPayload = append(grPayload, encodeAPFString("")...)
	grPayload = append(grPayload, encodeUint32(16992)...)
	grMsg := append([]byte{APFGlobalRequest}, grPayload...)
	_, err = conn.Write(grMsg)
	require.NoError(t, err)
	expectMessage(t, conn, APFRequestSuccess)

	// Consume the KeepaliveOptionsRequest sent by the server after handshake.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(2*time.Second)))
	expectMessage(t, conn, APFKeepaliveOptionsRequest)
	require.NoError(t, conn.SetReadDeadline(time.Time{}))
}

// connectedAMT starts a server, dials it, and completes the CIRA handshake,
// returning the server, the client conn, and the device UUID. The server is
// stopped on test cleanup.
func connectedAMT(t *testing.T) (*Server, net.Conn, uuid.UUID) {
	t.Helper()
	srv, _, caPEM := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	t.Cleanup(cancel)

	amtUUID := uuid.New()
	conn := connectAMT(t, addr, caPEM)
	simulateCIRA(t, conn, amtUUID)
	// The CIRA handshake completes on the server's reader goroutine. Wait for the
	// registered connection rather than guessing a sleep duration: callers can
	// then read server-side state immediately.
	require.Eventually(t, func() bool { return srv.GetConn(amtUUID) != nil },
		2*time.Second, 5*time.Millisecond, "server should register the CIRA connection")
	return srv, conn, amtUUID
}

// expectMessage reads one APF message from conn, asserts its type, and returns
// the raw payload.
func expectMessage(t *testing.T, conn net.Conn, want byte) []byte {
	t.Helper()
	msgType, raw, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, want, msgType)
	return raw
}

// requireConn returns the live MPS connection for id, failing if absent.
func requireConn(t *testing.T, srv *Server, id uuid.UUID) *Conn {
	t.Helper()
	mc := srv.GetConn(id)
	require.NotNil(t, mc)
	return mc
}

// channelCount returns the number of open channels on mc (lock-guarded).
func channelCount(mc *Conn) int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.channels)
}

// boundPorts returns a copy of mc's bound ports (lock-guarded).
func boundPorts(mc *Conn) []BoundPort {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return append([]BoundPort(nil), mc.BoundPorts...)
}

// sendWindow returns ch's current send window (lock-guarded).
func sendWindow(ch *Channel) int64 {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.sendWindow
}

// buildChannelOpen builds a forwarded-tcpip APFChannelOpen payload with the
// given sender channel, initial window, and max packet size. The connected and
// origin addresses are fixed — the server does not assert on them.
func buildChannelOpen(senderCh, window, maxPacket uint32) []byte {
	p := encodeAPFString("forwarded-tcpip")
	p = append(p, encodeUint32(senderCh)...)
	p = append(p, encodeUint32(window)...)
	p = append(p, encodeUint32(maxPacket)...)
	p = append(p, encodeAPFString("1.2.3.4")...)
	p = append(p, encodeUint32(16992)...)
	p = append(p, encodeAPFString("0.0.0.0")...)
	p = append(p, encodeUint32(0)...)
	return p
}

func TestMPSServerStartStop(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe(ctx, "127.0.0.1:0")
	}()

	addr := srv.Addr()
	assert.NotEmpty(t, addr)
	assert.Equal(t, 0, srv.ConnectedDeviceCount())

	cancel()
	err := <-done
	assert.NoError(t, err)
}

func TestMPSCIRAHandshake(t *testing.T) {
	srv, store, caPEM := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr, caPEM)

	simulateCIRA(t, conn, amtUUID)

	// Registration (conn map + online upsert) is async; poll instead of
	// assuming a fixed delay so the test is deterministic under -race.
	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 1 && srv.GetConn(amtUUID) != nil
	}, 2*time.Second, 5*time.Millisecond, "server should register the CIRA connection")
	require.Eventually(t, func() bool {
		status, ok := store.status(amtUUID)
		return ok && status == db.StatusOnline
	}, 2*time.Second, 5*time.Millisecond, "device should be upserted online")

	// Disconnect — count, conn map and the offline upsert all settle async.
	conn.Close()
	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 0 && srv.GetConn(amtUUID) == nil
	}, 2*time.Second, 5*time.Millisecond, "server should drop the closed connection")
	require.Eventually(t, func() bool {
		status, ok := store.status(amtUUID)
		return ok && status == db.StatusOffline
	}, 2*time.Second, 5*time.Millisecond, "device should be marked offline")
}

func TestMPSMultipleConnections(t *testing.T) {
	srv, _, caPEM := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	uuid1 := uuid.New()
	uuid2 := uuid.New()

	conn1 := connectAMT(t, addr, caPEM)
	simulateCIRA(t, conn1, uuid1)

	conn2 := connectAMT(t, addr, caPEM)
	simulateCIRA(t, conn2, uuid2)

	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 2
	}, 2*time.Second, 5*time.Millisecond, "both connections should register")

	conn1.Close()
	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 1 &&
			srv.GetConn(uuid1) == nil && srv.GetConn(uuid2) != nil
	}, 2*time.Second, 5*time.Millisecond, "only conn1 should be dropped")
}

func TestMPSBadHandshake(t *testing.T) {
	srv, _, caPEM := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	t.Run("wrong first message type", func(t *testing.T) {
		conn := connectAMT(t, addr, caPEM)
		// Send a service request instead of protocol version.
		require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServiceAuth))
		// Server should close the connection (async).
		require.Eventually(t, func() bool {
			return srv.ConnectedDeviceCount() == 0
		}, 2*time.Second, 5*time.Millisecond, "server should reject the bad handshake")
	})

	t.Run("garbage data", func(t *testing.T) {
		conn := connectAMT(t, addr, caPEM)
		_, err := conn.Write([]byte{0xFF, 0xFF, 0xFF})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return srv.ConnectedDeviceCount() == 0
		}, 2*time.Second, 5*time.Millisecond, "server should reject garbage data")
	})
}

func TestMPSChannelOpenClose(t *testing.T) {
	srv, conn, amtUUID := connectedAMT(t)

	// Send a channel open from "AMT device" side.
	msg := append([]byte{APFChannelOpen}, buildChannelOpen(7, 0x4000, 0x4000)...)
	_, err := conn.Write(msg)
	require.NoError(t, err)

	// Read channel open confirmation.
	expectMessage(t, conn, APFChannelOpenConfirm)

	// Send channel close.
	mc := requireConn(t, srv, amtUUID)
	assert.Equal(t, 1, channelCount(mc))

	// Close channel from AMT side — send close for server's local channel 0.
	require.NoError(t, WriteChannelClose(conn, 0))

	// Read the close response.
	expectMessage(t, conn, APFChannelClose)

	require.Eventually(t, func() bool { return channelCount(mc) == 0 },
		2*time.Second, 5*time.Millisecond, "server should drop the closed channel")
}

func TestHandshakeTracksFirstBoundPort(t *testing.T) {
	srv, _, amtUUID := connectedAMT(t)

	mc := requireConn(t, srv, amtUUID)
	require.Eventually(t, func() bool { return len(boundPorts(mc)) == 1 },
		2*time.Second, 5*time.Millisecond, "handshake should bind exactly one port")
	assert.Equal(t, uint32(16992), boundPorts(mc)[0].Port)
}

func TestMessageLoopTracksAdditionalPorts(t *testing.T) {
	srv, conn, amtUUID := connectedAMT(t)

	// Send additional tcpip-forward requests (like real AMT does).
	for _, port := range []uint32{16993, 5900} {
		grPayload := encodeAPFString("tcpip-forward")
		grPayload = append(grPayload, 1) // want_reply
		grPayload = append(grPayload, encodeAPFString("")...)
		grPayload = append(grPayload, encodeUint32(port)...)
		_, err := conn.Write(append([]byte{APFGlobalRequest}, grPayload...))
		require.NoError(t, err)

		expectMessage(t, conn, APFRequestSuccess)
	}

	mc := requireConn(t, srv, amtUUID)
	require.Eventually(t, func() bool { return len(boundPorts(mc)) == 3 },
		2*time.Second, 5*time.Millisecond, "server should track all three forwarded ports")

	ports := boundPorts(mc)
	assert.Equal(t, uint32(16992), ports[0].Port)
	assert.Equal(t, uint32(16993), ports[1].Port)
	assert.Equal(t, uint32(5900), ports[2].Port)
}

func TestChannelDataSendsWindowAdjust(t *testing.T) {
	_, conn, _ := connectedAMT(t)

	// Open a channel from AMT side (window 32K).
	_, err := conn.Write(append([]byte{APFChannelOpen}, buildChannelOpen(7, 0x8000, 0x8000)...))
	require.NoError(t, err)

	// Read channel open confirmation.
	expectMessage(t, conn, APFChannelOpenConfirm)

	// Send enough channel data to trigger a WindowAdj from the server.
	// Server should send WindowAdj when recvConsumed >= DefaultWindowSize/2 (16K).
	bigData := make([]byte, DefaultWindowSize/2+1)
	require.NoError(t, WriteChannelData(conn, 0, bigData)) // server's local ch = 0

	// Read the WindowAdj the server should send back.
	raw := expectMessage(t, conn, APFChannelWindowAdj)
	require.Len(t, raw, 8)
	adjustBytes := binary.BigEndian.Uint32(raw[4:8])
	assert.True(t, adjustBytes > 0, "window adjust should be positive")
}

func TestChannelWindowAdjIncrementsSendWindow(t *testing.T) {
	srv, conn, amtUUID := connectedAMT(t)

	// Open a channel from AMT side with a small window=1024.
	_, err := conn.Write(append([]byte{APFChannelOpen}, buildChannelOpen(5, 1024, 0x8000)...))
	require.NoError(t, err)

	expectMessage(t, conn, APFChannelOpenConfirm)

	mc := requireConn(t, srv, amtUUID)
	mc.mu.RLock()
	ch := mc.channels[0]
	mc.mu.RUnlock()
	require.NotNil(t, ch)

	assert.Equal(t, int64(1024), sendWindow(ch))

	// Send a WindowAdj from AMT side to increase the server's send window.
	require.NoError(t, WriteChannelWindowAdj(conn, 0, 4096))
	require.Eventually(t, func() bool { return sendWindow(ch) == int64(1024+4096) },
		2*time.Second, 5*time.Millisecond, "WindowAdj should raise the server's send window")
}

func TestKeepaliveRequestReplyEcho(t *testing.T) {
	_, conn, _ := connectedAMT(t)

	// Send a keepalive request from "AMT" side; server should echo the cookie back.
	var reqBuf [5]byte
	reqBuf[0] = APFKeepaliveRequest
	binary.BigEndian.PutUint32(reqBuf[1:], 0x12345678)
	_, err := conn.Write(reqBuf[:])
	require.NoError(t, err)

	raw := expectMessage(t, conn, APFKeepaliveReply)
	require.Len(t, raw, 4)
	assert.Equal(t, uint32(0x12345678), binary.BigEndian.Uint32(raw))
}

func TestKeepaliveOptionsNegotiation(t *testing.T) {
	// simulateCIRA already verifies the server sends KeepaliveOptionsRequest.
	// This test additionally verifies the handshake completes with keepalive active.
	srv, _, _ := connectedAMT(t)

	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 1
	}, 2*time.Second, 5*time.Millisecond, "handshake should complete with keepalive active")
}

// toIntelGUID encodes a standard UUID into Intel mixed-endian wire format
// (inverse of ReorderIntelGUID).
func toIntelGUID(u uuid.UUID) [16]byte {
	var raw [16]byte
	raw[0], raw[1], raw[2], raw[3] = u[3], u[2], u[1], u[0]
	raw[4], raw[5] = u[5], u[4]
	raw[6], raw[7] = u[7], u[6]
	copy(raw[8:], u[8:16])
	return raw
}

func TestConnNetConn(t *testing.T) {
	_, server := newDeadlinePipe(t)

	c := &Conn{netConn: server}
	assert.Same(t, server, c.NetConn())
}

func TestChannelSetOnData(t *testing.T) {
	ch := &Channel{}
	assert.Nil(t, ch.OnData)

	var got []byte
	ch.SetOnData(func(b []byte) { got = b })
	require.NotNil(t, ch.OnData)

	ch.OnData([]byte("hello"))
	assert.Equal(t, []byte("hello"), got)

	// Overwrite with nil should also be allowed.
	ch.SetOnData(nil)
	assert.Nil(t, ch.OnData)
}

func TestWriteChannelOpenDirect(t *testing.T) {
	var buf bytes.Buffer
	const senderCh uint32 = 42
	const addr = "10.0.0.1"
	const port uint16 = 16992

	require.NoError(t, writeChannelOpenDirect(&buf, senderCh, addr, port))

	out := buf.Bytes()
	require.NotEmpty(t, out)
	assert.Equal(t, APFChannelOpen, out[0])

	// type string
	typeLen := binary.BigEndian.Uint32(out[1:5])
	assert.Equal(t, uint32(len("direct-tcpip")), typeLen)
	off := 5 + int(typeLen)
	assert.Equal(t, "direct-tcpip", string(out[5:off]))

	// sender channel
	assert.Equal(t, senderCh, binary.BigEndian.Uint32(out[off:off+4]))
	off += 4

	// window + max packet
	assert.Equal(t, DefaultWindowSize, binary.BigEndian.Uint32(out[off:off+4]))
	off += 4
	assert.Equal(t, DefaultMaxPacketSize, binary.BigEndian.Uint32(out[off:off+4]))
	off += 4

	// connected address
	addrLen := binary.BigEndian.Uint32(out[off : off+4])
	off += 4
	assert.Equal(t, uint32(len(addr)), addrLen)
	assert.Equal(t, addr, string(out[off:off+int(addrLen)]))
	off += int(addrLen)

	// connected port
	assert.Equal(t, uint32(port), binary.BigEndian.Uint32(out[off:off+4]))
	off += 4

	// origin address "0.0.0.0"
	origLen := binary.BigEndian.Uint32(out[off : off+4])
	off += 4
	assert.Equal(t, uint32(len("0.0.0.0")), origLen)
	assert.Equal(t, "0.0.0.0", string(out[off:off+int(origLen)]))
	off += int(origLen)

	// origin port = 0
	assert.Equal(t, uint32(0), binary.BigEndian.Uint32(out[off:off+4]))
	off += 4

	assert.Equal(t, len(out), off)
}

// errWriter always fails, to exercise the error branch in writeChannelOpenDirect.
type errWriter struct{}

func (errWriter) Write(_ []byte) (int, error) { return 0, io.ErrClosedPipe }

func TestWriteChannelOpenDirectWriteError(t *testing.T) {
	err := writeChannelOpenDirect(errWriter{}, 1, "host", 80)
	assert.ErrorIs(t, err, io.ErrClosedPipe)
}

// TestUnlinkedConnectionIsAdoptedOnRetry proves the retry loop: a connection the
// server could not resolve at handshake time is picked up on a later tick, once
// the device claims its system UUID — no reconnect required.
func TestUnlinkedConnectionIsAdoptedOnRetry(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	state := newMemAMTState()
	linker := newMemLinker(false)
	srv := NewServer(cm, state, linker, discardLogger())
	// Pace the retry for the test rather than waiting out the production tick.
	srv.relinkInterval = 20 * time.Millisecond

	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr, cm.CACertPEM())
	simulateCIRA(t, conn, amtUUID)

	require.Eventually(t, func() bool { return srv.GetConn(amtUUID) != nil },
		2*time.Second, 5*time.Millisecond, "server should hold the unmatched connection")
	_, persisted := state.status(amtUUID)
	assert.False(t, persisted, "an unmatched connection must persist nothing")

	// The agent registers now.
	linker.arm()

	require.Eventually(t, func() bool {
		status, ok := state.status(amtUUID)
		return ok && status == db.StatusOnline
	}, 3*time.Second, 10*time.Millisecond, "the retry should adopt the connection and record it online")

	state.mu.Lock()
	defer state.mu.Unlock()
	require.Len(t, state.upserted, 1)
	assert.Equal(t, linker.deviceID, state.upserted[0].DeviceID,
		"the adopted row should point at the device that claimed the system UUID")
}
