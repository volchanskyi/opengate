package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testpg"
)

// pgAMTState is a tiny test-only AMTStateWriter that also reads back rows.
// Defined here (not in amt/) because amt imports mps; importing amt from
// mps_test.go would create a build cycle. Production wiring threads
// amt.PostgresAMTDevices through main.go instead.
type pgAMTState struct{ pool *db.PostgresStore }

func (p *pgAMTState) Upsert(ctx context.Context, d *db.AMTDevice) error {
	_, err := p.pool.DB().ExecContext(ctx,
		`INSERT INTO amt_devices (uuid, org_id, hostname, model, firmware, status, last_seen)
		 VALUES ($1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (uuid) DO UPDATE SET
		   org_id    = EXCLUDED.org_id,
		   hostname  = CASE WHEN EXCLUDED.hostname = '' THEN amt_devices.hostname ELSE EXCLUDED.hostname END,
		   model     = CASE WHEN EXCLUDED.model    = '' THEN amt_devices.model    ELSE EXCLUDED.model    END,
		   firmware  = CASE WHEN EXCLUDED.firmware = '' THEN amt_devices.firmware ELSE EXCLUDED.firmware END,
		   status    = EXCLUDED.status,
		   last_seen = NOW()`,
		d.UUID, dbtx.DefaultOrgID, d.Hostname, d.Model, d.Firmware, string(d.Status))
	return err
}

func (p *pgAMTState) SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error {
	_, err := p.pool.DB().ExecContext(ctx,
		`UPDATE amt_devices SET status = $1, last_seen = NOW() WHERE org_id = $2 AND uuid = $3`,
		string(status), dbtx.DefaultOrgID, id)
	return err
}

func (p *pgAMTState) Get(ctx context.Context, id uuid.UUID) (*db.AMTDevice, error) {
	var d db.AMTDevice
	err := p.pool.DB().QueryRowContext(ctx,
		`SELECT uuid, hostname, model, firmware, status, last_seen FROM amt_devices WHERE org_id = $1 AND uuid = $2`,
		dbtx.DefaultOrgID, id).Scan(&d.UUID, &d.Hostname, &d.Model, &d.Firmware, &d.Status, &d.LastSeen)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) (*Server, *pgAMTState) {
	t.Helper()

	store, err := db.NewPostgresStore(t.Context(), testpg.BaseURL(t))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	logger := discardLogger()
	state := &pgAMTState{pool: store}
	srv := NewServer(cm, state, logger)
	return srv, state
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

func connectAMT(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 5 * time.Second},
		"tcp", addr,
		&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // test only
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
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	t.Cleanup(cancel)

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)
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
	srv, _ := newTestServer(t)
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
	srv, store := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)

	simulateCIRA(t, conn, amtUUID)

	// Registration (conn map + online upsert) is async; poll instead of
	// assuming a fixed delay so the test is deterministic under -race.
	ctx := context.Background()
	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 1 && srv.GetConn(amtUUID) != nil
	}, 2*time.Second, 5*time.Millisecond, "server should register the CIRA connection")
	require.Eventually(t, func() bool {
		device, err := store.Get(ctx, amtUUID)
		return err == nil && device.Status == db.StatusOnline
	}, 2*time.Second, 5*time.Millisecond, "device should be upserted online")

	// Disconnect — count, conn map and the offline upsert all settle async.
	conn.Close()
	require.Eventually(t, func() bool {
		return srv.ConnectedDeviceCount() == 0 && srv.GetConn(amtUUID) == nil
	}, 2*time.Second, 5*time.Millisecond, "server should drop the closed connection")
	require.Eventually(t, func() bool {
		device, err := store.Get(ctx, amtUUID)
		return err == nil && device.Status == db.StatusOffline
	}, 2*time.Second, 5*time.Millisecond, "device should be marked offline")
}

func TestMPSMultipleConnections(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	uuid1 := uuid.New()
	uuid2 := uuid.New()

	conn1 := connectAMT(t, addr)
	simulateCIRA(t, conn1, uuid1)

	conn2 := connectAMT(t, addr)
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
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	t.Run("wrong first message type", func(t *testing.T) {
		conn := connectAMT(t, addr)
		// Send a service request instead of protocol version.
		require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServiceAuth))
		// Server should close the connection (async).
		require.Eventually(t, func() bool {
			return srv.ConnectedDeviceCount() == 0
		}, 2*time.Second, 5*time.Millisecond, "server should reject the bad handshake")
	})

	t.Run("garbage data", func(t *testing.T) {
		conn := connectAMT(t, addr)
		_, err := conn.Write([]byte{0xFF, 0xFF, 0xFF})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return srv.ConnectedDeviceCount() == 0
		}, 2*time.Second, 5*time.Millisecond, "server should reject garbage data")
	})
}

func TestMPSChannelOpenClose(t *testing.T) {
	srv, conn, amtUUID := connectedAMT(t)
	time.Sleep(50 * time.Millisecond)

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

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, channelCount(mc))
}

func TestHandshakeTracksFirstBoundPort(t *testing.T) {
	srv, _, amtUUID := connectedAMT(t)
	time.Sleep(50 * time.Millisecond)

	ports := boundPorts(requireConn(t, srv, amtUUID))
	require.Len(t, ports, 1)
	assert.Equal(t, uint32(16992), ports[0].Port)
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

	time.Sleep(50 * time.Millisecond)

	ports := boundPorts(requireConn(t, srv, amtUUID))
	require.Len(t, ports, 3)
	assert.Equal(t, uint32(16992), ports[0].Port)
	assert.Equal(t, uint32(16993), ports[1].Port)
	assert.Equal(t, uint32(5900), ports[2].Port)
}

func TestChannelDataSendsWindowAdjust(t *testing.T) {
	_, conn, _ := connectedAMT(t)
	time.Sleep(50 * time.Millisecond)

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
	time.Sleep(50 * time.Millisecond)

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
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int64(1024+4096), sendWindow(ch))
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
	client, server := net.Pipe()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
	})

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
