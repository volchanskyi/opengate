package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const testHost = "test-host"

// newTestAgentCert creates a cert manager and signs an agent cert for it,
// returning the manager, device ID, and the agent's DER-encoded certificate.
func newTestAgentCert(t *testing.T) (*cert.Manager, string, []byte) {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	deviceID := uuid.New().String()
	tlsCert, err := cm.SignAgent(deviceID, testHost)
	require.NoError(t, err)

	return cm, deviceID, tlsCert.Certificate[0]
}

// writeAgentHello encodes and writes an AgentHello with a fresh random nonce
// and the given cert hash. The agent opens the stream and writes first, so
// callers use this before reading anything from the server.
func writeAgentHello(t *testing.T, w io.Writer, agentCertHash [48]byte) {
	t.Helper()
	var nonce [32]byte
	_, err := rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = w.Write(protocol.EncodeAgentHello(nonce, agentCertHash))
	require.NoError(t, err)
}

// handshakeResult is the outcome of an async PerformHandshake call.
type handshakeResult struct {
	hr  *HandshakeResult
	err error
}

// runHandshakeAsync runs h.PerformHandshake on serverConn in a goroutine
// (bounded by the given timeout) and returns a channel that receives its
// outcome.
func runHandshakeAsync(h *Handshaker, serverConn net.Conn, peerCerts [][]byte, timeout time.Duration) <-chan handshakeResult {
	ch := make(chan handshakeResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		hr, err := h.PerformHandshake(ctx, serverConn, peerCerts)
		ch <- handshakeResult{hr, err}
	}()
	return ch
}

// newTestHandshaker creates a cert manager and a Handshaker over it.
func newTestHandshaker(t *testing.T) *Handshaker {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	return NewHandshaker(cm)
}

func TestHandshaker_FullExchange(t *testing.T) {
	cm, deviceID, agentCertDER := newTestAgentCert(t)
	h := NewHandshaker(cm)

	// Create a pipe to simulate a QUIC stream
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	ch := runHandshakeAsync(h, serverConn, [][]byte{agentCertDER}, 5*time.Second)

	// Client (agent) side: the agent opened the stream, so it writes first.
	writeAgentHello(t, clientConn, sha512.Sum384(agentCertDER))

	// Now read the server's reply.
	serverHelloBuf := make([]byte, 81)
	_, err := io.ReadFull(clientConn, serverHelloBuf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.MsgServerHello), serverHelloBuf[0])

	// Get result
	res := <-ch
	require.NoError(t, res.err)
	require.NotNil(t, res.hr)
	assert.Equal(t, deviceID, res.hr.DeviceID.String())
	assert.Equal(t, agentCertDER, res.hr.AgentCertDER)
	assert.False(t, res.hr.Skipped)
}

func TestHandshaker_WrongCertHash(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	h := NewHandshaker(cm)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	ch := runHandshakeAsync(h, serverConn, [][]byte{agentCertDER}, 5*time.Second)

	// Agent opens + writes first, with a WRONG cert hash (all zeros).
	writeAgentHello(t, clientConn, [48]byte{})

	res := <-ch
	assert.Error(t, res.err)
	assert.True(t, errors.Is(res.err, ErrHandshakeFailed))
}

func TestHandshaker_EmptyStream(t *testing.T) {
	h := newTestHandshaker(t)

	// Create pipe and close client side immediately
	serverConn, clientConn := net.Pipe()
	clientConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := h.PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
	serverConn.Close()
}

func TestHandshaker_Timeout(t *testing.T) {
	h := newTestHandshaker(t)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Use a very short timeout — client never sends AgentHello, so the
	// server's initial read blocks until the deadline fires.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := h.PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
}

func TestHandshaker_ServerHelloContainsValidCACertHash(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	h := NewHandshaker(cm)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Start handshake in goroutine. The server now reads AgentHello first
	// and only writes ServerHello once the agent's cert hash validates.
	runHandshakeAsync(h, serverConn, [][]byte{agentCertDER}, 2*time.Second)

	// Agent opens + writes first.
	writeAgentHello(t, clientConn, sha512.Sum384(agentCertDER))

	// Read ServerHello and verify cert hash matches the CA
	buf := make([]byte, 81)
	_, err := io.ReadFull(clientConn, buf)
	require.NoError(t, err)

	assert.Equal(t, byte(protocol.MsgServerHello), buf[0])

	var receivedHash [48]byte
	copy(receivedHash[:], buf[33:81])

	expectedHash := sha512.Sum384(cm.CACert().Raw)
	assert.Equal(t, expectedHash, receivedHash)
}
