package mps

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestServer(t *testing.T) (*Server, *db.SQLiteStore) {
	t.Helper()
	dir := t.TempDir()
	cm, err := cert.NewManager(dir)
	require.NoError(t, err)

	store, err := db.NewSQLiteStore(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	logger := discardLogger()
	srv := NewServer(cm, store, logger)
	return srv, store
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
	msgType, _, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFProtocolVersion, msgType)

	// Step 2: Send ServiceRequest (auth).
	require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServiceAuth))
	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFServiceAccept, msgType)

	// Step 3: Send UserAuthRequest.
	authPayload := encodeAPFString("admin")
	authPayload = append(authPayload, encodeAPFString(ServiceAuth)...)
	authPayload = append(authPayload, encodeAPFString("digest")...)
	msg := append([]byte{APFUserAuthRequest}, authPayload...)
	_, err = conn.Write(msg)
	require.NoError(t, err)

	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFUserAuthSuccess, msgType)

	// Step 4: Send ServiceRequest (pfwd).
	require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServicePFwd))
	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFServiceAccept, msgType)

	// Step 5: Send GlobalRequest (tcpip-forward) for port 16992.
	grPayload := encodeAPFString("tcpip-forward")
	grPayload = append(grPayload, 1) // want_reply
	grPayload = append(grPayload, encodeAPFString("")...)
	grPayload = append(grPayload, encodeUint32(16992)...)
	grMsg := append([]byte{APFGlobalRequest}, grPayload...)
	_, err = conn.Write(grMsg)
	require.NoError(t, err)

	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFRequestSuccess, msgType)

	// Consume the KeepaliveOptionsRequest sent by the server after handshake.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second)) //nolint:errcheck
	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFKeepaliveOptionsRequest, msgType)
	conn.SetReadDeadline(time.Time{}) //nolint:errcheck
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

	// Give server a moment to register.
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, srv.ConnectedDeviceCount())
	assert.NotNil(t, srv.GetConn(amtUUID))

	// Check DB upsert.
	ctx := context.Background()
	device, err := store.GetAMTDevice(ctx, amtUUID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOnline, device.Status)

	// Disconnect and verify cleanup.
	conn.Close()
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 0, srv.ConnectedDeviceCount())
	assert.Nil(t, srv.GetConn(amtUUID))

	device, err = store.GetAMTDevice(ctx, amtUUID)
	require.NoError(t, err)
	assert.Equal(t, db.StatusOffline, device.Status)
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

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 2, srv.ConnectedDeviceCount())

	conn1.Close()
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, srv.ConnectedDeviceCount())
	assert.Nil(t, srv.GetConn(uuid1))
	assert.NotNil(t, srv.GetConn(uuid2))
}

func TestMPSBadHandshake(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	t.Run("wrong first message type", func(t *testing.T) {
		conn := connectAMT(t, addr)
		// Send a service request instead of protocol version.
		require.NoError(t, writeStringMsg(conn, APFServiceRequest, ServiceAuth))
		// Server should close the connection.
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 0, srv.ConnectedDeviceCount())
	})

	t.Run("garbage data", func(t *testing.T) {
		conn := connectAMT(t, addr)
		_, err := conn.Write([]byte{0xFF, 0xFF, 0xFF})
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)
		assert.Equal(t, 0, srv.ConnectedDeviceCount())
	})
}

func TestMPSChannelOpenClose(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)

	time.Sleep(50 * time.Millisecond)

	// Send a channel open from "AMT device" side.
	chOpenPayload := encodeAPFString("forwarded-tcpip")
	chOpenPayload = append(chOpenPayload, encodeUint32(7)...)     // sender channel
	chOpenPayload = append(chOpenPayload, encodeUint32(0x4000)...) // window
	chOpenPayload = append(chOpenPayload, encodeUint32(0x4000)...) // max packet
	// Add connected address and origin for "forwarded-tcpip"
	chOpenPayload = append(chOpenPayload, encodeAPFString("192.168.1.1")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(16993)...)
	chOpenPayload = append(chOpenPayload, encodeAPFString("0.0.0.0")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(0)...)

	msg := append([]byte{APFChannelOpen}, chOpenPayload...)
	_, err := conn.Write(msg)
	require.NoError(t, err)

	// Read channel open confirmation.
	msgType, _, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpenConfirm, msgType)

	// Send channel close.
	mc := srv.GetConn(amtUUID)
	require.NotNil(t, mc)

	mc.mu.RLock()
	chanCount := len(mc.channels)
	mc.mu.RUnlock()
	assert.Equal(t, 1, chanCount)

	// Close channel from AMT side — send close for server's local channel 0.
	require.NoError(t, WriteChannelClose(conn, 0))

	// Read the close response.
	msgType, _, err = ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFChannelClose, msgType)

	time.Sleep(50 * time.Millisecond)

	mc.mu.RLock()
	chanCount = len(mc.channels)
	mc.mu.RUnlock()
	assert.Equal(t, 0, chanCount)
}

func TestHandshakeTracksFirstBoundPort(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)

	time.Sleep(50 * time.Millisecond)

	mc := srv.GetConn(amtUUID)
	require.NotNil(t, mc)
	mc.mu.RLock()
	ports := append([]BoundPort(nil), mc.BoundPorts...)
	mc.mu.RUnlock()

	require.Len(t, ports, 1)
	assert.Equal(t, uint32(16992), ports[0].Port)
}

func TestMessageLoopTracksAdditionalPorts(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)

	// Send additional tcpip-forward requests (like real AMT does).
	for _, port := range []uint32{16993, 5900} {
		grPayload := encodeAPFString("tcpip-forward")
		grPayload = append(grPayload, 1) // want_reply
		grPayload = append(grPayload, encodeAPFString("")...)
		grPayload = append(grPayload, encodeUint32(port)...)
		_, err := conn.Write(append([]byte{APFGlobalRequest}, grPayload...))
		require.NoError(t, err)

		msgType, _, err := ReadMessage(conn)
		require.NoError(t, err)
		assert.Equal(t, APFRequestSuccess, msgType)
	}

	time.Sleep(50 * time.Millisecond)

	mc := srv.GetConn(amtUUID)
	require.NotNil(t, mc)
	mc.mu.RLock()
	ports := append([]BoundPort(nil), mc.BoundPorts...)
	mc.mu.RUnlock()

	require.Len(t, ports, 3)
	assert.Equal(t, uint32(16992), ports[0].Port)
	assert.Equal(t, uint32(16993), ports[1].Port)
	assert.Equal(t, uint32(5900), ports[2].Port)
}

func TestChannelDataSendsWindowAdjust(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)
	time.Sleep(50 * time.Millisecond)

	// Open a channel from AMT side.
	chOpenPayload := encodeAPFString("forwarded-tcpip")
	chOpenPayload = append(chOpenPayload, encodeUint32(7)...)         // sender channel
	chOpenPayload = append(chOpenPayload, encodeUint32(0x8000)...)    // window 32K
	chOpenPayload = append(chOpenPayload, encodeUint32(0x8000)...)    // max packet
	chOpenPayload = append(chOpenPayload, encodeAPFString("1.2.3.4")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(16992)...)
	chOpenPayload = append(chOpenPayload, encodeAPFString("0.0.0.0")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(0)...)
	_, err := conn.Write(append([]byte{APFChannelOpen}, chOpenPayload...))
	require.NoError(t, err)

	// Read channel open confirmation.
	msgType, _, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpenConfirm, msgType)

	// Send enough channel data to trigger a WindowAdj from the server.
	// Server should send WindowAdj when recvConsumed >= DefaultWindowSize/2 (16K).
	bigData := make([]byte, DefaultWindowSize/2+1)
	require.NoError(t, WriteChannelData(conn, 0, bigData)) // server's local ch = 0

	// Read the WindowAdj the server should send back.
	msgType, raw, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFChannelWindowAdj, msgType)
	require.Len(t, raw, 8)
	adjustBytes := binary.BigEndian.Uint32(raw[4:8])
	assert.True(t, adjustBytes > 0, "window adjust should be positive")
}

func TestChannelWindowAdjIncrementsSendWindow(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)
	time.Sleep(50 * time.Millisecond)

	// Open a channel from AMT side with window=1024.
	chOpenPayload := encodeAPFString("forwarded-tcpip")
	chOpenPayload = append(chOpenPayload, encodeUint32(5)...)       // sender channel
	chOpenPayload = append(chOpenPayload, encodeUint32(1024)...)    // small window
	chOpenPayload = append(chOpenPayload, encodeUint32(0x8000)...)  // max packet
	chOpenPayload = append(chOpenPayload, encodeAPFString("1.2.3.4")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(16992)...)
	chOpenPayload = append(chOpenPayload, encodeAPFString("0.0.0.0")...)
	chOpenPayload = append(chOpenPayload, encodeUint32(0)...)
	_, err := conn.Write(append([]byte{APFChannelOpen}, chOpenPayload...))
	require.NoError(t, err)

	msgType, _, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpenConfirm, msgType)

	mc := srv.GetConn(amtUUID)
	require.NotNil(t, mc)

	mc.mu.RLock()
	ch := mc.channels[0]
	mc.mu.RUnlock()
	require.NotNil(t, ch)

	ch.mu.Lock()
	initialWindow := ch.sendWindow
	ch.mu.Unlock()
	assert.Equal(t, int64(1024), initialWindow)

	// Send a WindowAdj from AMT side to increase the server's send window.
	require.NoError(t, WriteChannelWindowAdj(conn, 0, 4096))
	time.Sleep(50 * time.Millisecond)

	ch.mu.Lock()
	newWindow := ch.sendWindow
	ch.mu.Unlock()
	assert.Equal(t, int64(1024+4096), newWindow)
}

func TestKeepaliveRequestReplyEcho(t *testing.T) {
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID) // consumes KeepaliveOptionsRequest

	// Send a keepalive request from "AMT" side; server should echo the cookie back.
	var reqBuf [5]byte
	reqBuf[0] = APFKeepaliveRequest
	binary.BigEndian.PutUint32(reqBuf[1:], 0x12345678)
	_, err := conn.Write(reqBuf[:])
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(conn)
	require.NoError(t, err)
	assert.Equal(t, APFKeepaliveReply, msgType)
	require.Len(t, raw, 4)
	assert.Equal(t, uint32(0x12345678), binary.BigEndian.Uint32(raw))
}

func TestKeepaliveOptionsNegotiation(t *testing.T) {
	// simulateCIRA already verifies the server sends KeepaliveOptionsRequest.
	// This test additionally verifies the handshake completes with keepalive active.
	srv, _ := newTestServer(t)
	addr, cancel := startTestServer(t, srv)
	defer cancel()

	amtUUID := uuid.New()
	conn := connectAMT(t, addr)
	simulateCIRA(t, conn, amtUUID)

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, srv.ConnectedDeviceCount())
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
