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

// writeSkipAuth encodes and writes a 0x14 SkipAuth fast-path message carrying
// the agent's cached CA cert hash.
func writeSkipAuth(t *testing.T, w io.Writer, cachedCAHash [48]byte) {
	t.Helper()
	_, err := w.Write(protocol.EncodeSkipAuth(cachedCAHash))
	require.NoError(t, err)
}

// caCertHash returns the SHA-384 of the manager's CA certificate — the value
// an agent caches and replays on the 0x14 fast path.
func caCertHash(cm *cert.Manager) [48]byte {
	return sha512.Sum384(cm.CACert().Raw)
}

// errReader always fails — used to exercise the nonce-generation error path.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("rand source failure") }

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

// handshakePipe wires a Handshaker to an in-memory net.Pipe and runs the
// handshake in the background, exposing the agent (client) end and the result.
type handshakePipe struct {
	cm           *cert.Manager
	deviceID     string
	agentCertDER []byte
	client       net.Conn
	result       <-chan handshakeResult
}

// newHandshakePipe sets up a handshake over the standard mTLS agent cert.
func newHandshakePipe(t *testing.T) *handshakePipe {
	t.Helper()
	cm, deviceID, agentCertDER := newTestAgentCert(t)
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })
	ch := runHandshakeAsync(NewHandshaker(cm), serverConn, [][]byte{agentCertDER}, 5*time.Second)
	return &handshakePipe{cm, deviceID, agentCertDER, clientConn, ch}
}

// readServerHello reads and returns the 81-byte ServerHello reply.
func (p *handshakePipe) readServerHello(t *testing.T) []byte {
	t.Helper()
	buf := make([]byte, 81)
	_, err := io.ReadFull(p.client, buf)
	require.NoError(t, err)
	require.Equal(t, byte(protocol.MsgServerHello), buf[0])
	return buf
}

func TestHandshaker_FullExchange(t *testing.T) {
	p := newHandshakePipe(t)

	// The agent opened the stream, so it writes AgentHello first.
	writeAgentHello(t, p.client, sha512.Sum384(p.agentCertDER))
	p.readServerHello(t)

	res := <-p.result
	require.NoError(t, res.err)
	require.NotNil(t, res.hr)
	assert.Equal(t, p.deviceID, res.hr.DeviceID.String())
	assert.Equal(t, p.agentCertDER, res.hr.AgentCertDER)
	assert.False(t, res.hr.Skipped)
}

func TestHandshaker_FastPath_ValidHash(t *testing.T) {
	p := newHandshakePipe(t)

	// SkipAuth with the current CA hash takes the fast path; no ServerHello
	// reply is expected.
	writeSkipAuth(t, p.client, caCertHash(p.cm))

	res := <-p.result
	require.NoError(t, res.err)
	require.NotNil(t, res.hr)
	assert.True(t, res.hr.Skipped, "valid cached hash must take the fast path")
	assert.Equal(t, p.deviceID, res.hr.DeviceID.String())
	assert.Equal(t, p.agentCertDER, res.hr.AgentCertDER)
}

func TestHandshaker_ServerHelloContainsValidCACertHash(t *testing.T) {
	p := newHandshakePipe(t)

	writeAgentHello(t, p.client, sha512.Sum384(p.agentCertDER))
	buf := p.readServerHello(t)

	var receivedHash [48]byte
	copy(receivedHash[:], buf[33:81])
	assert.Equal(t, caCertHash(p.cm), receivedHash)
}

func TestHandshaker_Timeout(t *testing.T) {
	// covers client-first accept + bounded timeout: a peer that opens no stream
	// or writes no handshake bytes must fail by context deadline, not hang forever.
	cm, _, _ := newTestAgentCert(t)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Client never writes, so the server's initial type-byte read blocks until
	// the deadline fires.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := NewHandshaker(cm).PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
}

func TestHandshaker_NonceGenerationError(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	// Inject a failing randomness source so ServerHello nonce generation errors.
	h := &Handshaker{cert: cm, rand: errReader{}}

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	ch := runHandshakeAsync(h, serverConn, [][]byte{agentCertDER}, 5*time.Second)
	writeAgentHello(t, clientConn, sha512.Sum384(agentCertDER))

	res := <-ch
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "nonce")
}

// runHandshakeRejectWithCM runs a handshake expected to fail: it writes the
// client side via clientWrite and returns the server-side error.
func runHandshakeRejectWithCM(t *testing.T, cm *cert.Manager, peerCerts [][]byte, clientWrite func(*testing.T, net.Conn)) error {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })
	ch := runHandshakeAsync(NewHandshaker(cm), serverConn, peerCerts, 5*time.Second)
	clientWrite(t, clientConn)
	return (<-ch).err
}

// TestHandshaker_Rejections covers every authentication/parse failure: each
// must return ErrHandshakeFailed (no auth weakening on either path).
func TestHandshaker_Rejections(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	validCAHash := caCertHash(cm)

	// A cert signed by the same CA but with a non-UUID CN.
	badCNCert, err := cm.SignAgent("not-a-uuid", testHost)
	require.NoError(t, err)
	badCNDER := badCNCert.Certificate[0]

	cases := []struct {
		name      string
		peerCerts [][]byte
		write     func(*testing.T, net.Conn)
	}{
		{"full path wrong cert hash", [][]byte{agentCertDER}, func(t *testing.T, c net.Conn) {
			writeAgentHello(t, c, [48]byte{})
		}},
		{"fast path stale hash", [][]byte{agentCertDER}, func(t *testing.T, c net.Conn) {
			writeSkipAuth(t, c, [48]byte{})
		}},
		{"fast path without peer cert", nil, func(t *testing.T, c net.Conn) {
			writeSkipAuth(t, c, validCAHash)
		}},
		{"unknown first message type", [][]byte{agentCertDER}, func(t *testing.T, c net.Conn) {
			_, err := c.Write([]byte{0x99})
			require.NoError(t, err)
		}},
		{"unparseable peer cert", [][]byte{[]byte("not-a-certificate")}, func(t *testing.T, c net.Conn) {
			writeAgentHello(t, c, validCAHash)
		}},
		{"non-uuid common name", [][]byte{badCNDER}, func(t *testing.T, c net.Conn) {
			writeAgentHello(t, c, sha512.Sum384(badCNDER))
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runHandshakeRejectWithCM(t, cm, tc.peerCerts, tc.write)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrHandshakeFailed), "want ErrHandshakeFailed, got %v", err)
		})
	}
}

// TestHandshaker_IOFailures covers transport-level failures (truncated or
// closed streams). These surface as plain I/O errors, not ErrHandshakeFailed.
// The write closure receives the agent cert DER so the ServerHello-write case
// can present a matching AgentHello and reach the reply step before closing.
func TestHandshaker_IOFailures(t *testing.T) {
	cases := []struct {
		name  string
		write func(t *testing.T, c net.Conn, agentCertDER []byte)
	}{
		{"client closes immediately", func(_ *testing.T, c net.Conn, _ []byte) {
			c.Close()
		}},
		{"truncated AgentHello body", func(t *testing.T, c net.Conn, _ []byte) {
			_, err := c.Write([]byte{protocol.MsgAgentHello})
			require.NoError(t, err)
			c.Close()
		}},
		{"truncated SkipAuth hash", func(t *testing.T, c net.Conn, _ []byte) {
			_, err := c.Write([]byte{protocol.MsgSkipAuth})
			require.NoError(t, err)
			c.Close()
		}},
		{"stream closes before ServerHello write", func(t *testing.T, c net.Conn, agentCertDER []byte) {
			// Valid AgentHello (hash matches peer cert) so the server reaches
			// the ServerHello write, which then fails on the closed stream.
			writeAgentHello(t, c, sha512.Sum384(agentCertDER))
			c.Close()
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cm, _, agentCertDER := newTestAgentCert(t)
			err := runHandshakeRejectWithCM(t, cm, [][]byte{agentCertDER}, func(t *testing.T, c net.Conn) {
				tc.write(t, c, agentCertDER)
			})
			require.Error(t, err)
		})
	}
}
