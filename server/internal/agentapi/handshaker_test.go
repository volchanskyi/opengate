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

const handshakeTestTimeout = 5 * time.Second

// newHandshakeConns returns a net.Pipe whose client end has a deadline; the pipe is unbuffered,
// so a stalled server would otherwise block the test's own Write or ReadFull forever.
func newHandshakeConns(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	require.NoError(t, clientConn.SetDeadline(time.Now().Add(handshakeTestTimeout)))
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })
	return serverConn, clientConn
}

func newTestAgentCert(t *testing.T) (*cert.Manager, string, []byte) {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	deviceID := uuid.New().String()
	tlsCert, err := cm.SignAgent(deviceID, testHost)
	require.NoError(t, err)

	return cm, deviceID, tlsCert.Certificate[0]
}

func writeAgentHello(t *testing.T, w io.Writer, agentCertHash [48]byte) {
	t.Helper()
	var nonce [32]byte
	_, err := rand.Read(nonce[:])
	require.NoError(t, err)
	_, err = w.Write(protocol.EncodeAgentHello(nonce, agentCertHash))
	require.NoError(t, err)
}

func writeSkipAuth(t *testing.T, w io.Writer, cachedCAHash [48]byte) {
	t.Helper()
	_, err := w.Write(protocol.EncodeSkipAuth(cachedCAHash))
	require.NoError(t, err)
}

func caCertHash(cm *cert.Manager) [48]byte {
	return sha512.Sum384(cm.CACert().Raw)
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("rand source failure") }

type handshakeResult struct {
	hr  *HandshakeResult
	err error
}

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

type handshakePipe struct {
	cm           *cert.Manager
	deviceID     string
	agentCertDER []byte
	client       net.Conn
	result       <-chan handshakeResult
}

func newHandshakePipe(t *testing.T) *handshakePipe {
	t.Helper()
	cm, deviceID, agentCertDER := newTestAgentCert(t)
	serverConn, clientConn := newHandshakeConns(t)
	ch := runHandshakeAsync(NewHandshaker(cm), serverConn, [][]byte{agentCertDER}, handshakeTestTimeout)
	return &handshakePipe{cm, deviceID, agentCertDER, clientConn, ch}
}

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
	cm, _, _ := newTestAgentCert(t)

	serverConn, _ := newHandshakeConns(t)

	// The client never writes, so the type-byte read blocks until the deadline fires.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := NewHandshaker(cm).PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
}

func TestHandshaker_NonceGenerationError(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	h := NewHandshaker(cm)
	h.rand = errReader{}

	serverConn, clientConn := newHandshakeConns(t)

	ch := runHandshakeAsync(h, serverConn, [][]byte{agentCertDER}, handshakeTestTimeout)
	writeAgentHello(t, clientConn, sha512.Sum384(agentCertDER))

	res := <-ch
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "nonce")
}

func runHandshakeRejectWithCM(t *testing.T, cm *cert.Manager, peerCerts [][]byte, clientWrite func(*testing.T, net.Conn)) error {
	t.Helper()
	serverConn, clientConn := newHandshakeConns(t)
	ch := runHandshakeAsync(NewHandshaker(cm), serverConn, peerCerts, handshakeTestTimeout)
	clientWrite(t, clientConn)
	return (<-ch).err
}

func TestHandshaker_Rejections(t *testing.T) {
	cm, _, agentCertDER := newTestAgentCert(t)
	validCAHash := caCertHash(cm)

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
			// A matching hash lets the server reach the ServerHello write, which fails when closed.
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

func TestHandshaker_CACertHashAgreesAcrossPaths(t *testing.T) {
	p := newHandshakePipe(t)
	live := caCertHash(p.cm)

	writeAgentHello(t, p.client, sha512.Sum384(p.agentCertDER))
	hello := p.readServerHello(t)
	res := <-p.result
	require.NoError(t, res.err)
	assert.Equal(t, live[:], hello[33:81], "ServerHello must carry the live CA cert hash")

	h := NewHandshaker(p.cm)
	for _, tc := range []struct {
		name    string
		hash    [48]byte
		wantErr bool
	}{
		{"live CA hash", live, false},
		{"another CA's hash", caCertHash(otherCAManager(t)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			serverConn, clientConn := newHandshakeConns(t)
			ch := runHandshakeAsync(h, serverConn, [][]byte{p.agentCertDER}, handshakeTestTimeout)
			writeSkipAuth(t, clientConn, tc.hash)
			got := <-ch
			if tc.wantErr {
				require.Error(t, got.err)
				assert.True(t, errors.Is(got.err, ErrHandshakeFailed))
				return
			}
			require.NoError(t, got.err)
			assert.True(t, got.hr.Skipped)
		})
	}
}

func otherCAManager(t *testing.T) *cert.Manager {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	return cm
}
