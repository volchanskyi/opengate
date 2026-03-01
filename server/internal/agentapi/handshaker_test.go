package agentapi

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// generateAgentCert creates a test agent cert signed by the given CA.
func generateAgentCert(t *testing.T, cm *cert.Manager, deviceID string) (certDER []byte, key *ecdsa.PrivateKey) {
	t.Helper()
	agentKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: deviceID},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, cm.CACert(), &agentKey.PublicKey, cm.CACert().PublicKey.(*ecdsa.PrivateKey))
	// CACert().PublicKey is the public key — need the CA private key.
	// Use cert.Manager.SignAgent instead.
	_ = der

	tlsCert, err := cm.SignAgent(deviceID, "test-host")
	require.NoError(t, err)
	return tlsCert.Certificate[0], nil
}

func TestHandshaker_FullExchange(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	deviceID := uuid.New().String()
	tlsCert, err := cm.SignAgent(deviceID, "test-host")
	require.NoError(t, err)
	agentCertDER := tlsCert.Certificate[0]

	h := NewHandshaker(cm)

	// Create a pipe to simulate a QUIC stream
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Run handshake server-side in a goroutine
	type result struct {
		hr  *HandshakeResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hr, err := h.PerformHandshake(ctx, serverConn, [][]byte{agentCertDER})
		ch <- result{hr, err}
	}()

	// Client side: read ServerHello, send AgentHello
	serverHelloBuf := make([]byte, 81)
	_, err = io.ReadFull(clientConn, serverHelloBuf)
	require.NoError(t, err)
	assert.Equal(t, byte(protocol.MsgServerHello), serverHelloBuf[0])

	// Compute agent cert hash
	agentCertHash := sha512.Sum384(agentCertDER)

	// Send AgentHello
	var nonce [32]byte
	copy(nonce[:], serverHelloBuf[1:33])
	agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
	_, err = clientConn.Write(agentHello)
	require.NoError(t, err)

	// Get result
	res := <-ch
	require.NoError(t, res.err)
	require.NotNil(t, res.hr)
	assert.Equal(t, deviceID, res.hr.DeviceID.String())
	assert.Equal(t, agentCertDER, res.hr.AgentCertDER)
	assert.False(t, res.hr.Skipped)
}

func TestHandshaker_WrongCertHash(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	deviceID := uuid.New().String()
	tlsCert, err := cm.SignAgent(deviceID, "test-host")
	require.NoError(t, err)
	agentCertDER := tlsCert.Certificate[0]

	h := NewHandshaker(cm)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	type result struct {
		hr  *HandshakeResult
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		hr, err := h.PerformHandshake(ctx, serverConn, [][]byte{agentCertDER})
		ch <- result{hr, err}
	}()

	// Read ServerHello
	serverHelloBuf := make([]byte, 81)
	_, err = io.ReadFull(clientConn, serverHelloBuf)
	require.NoError(t, err)

	// Send AgentHello with WRONG cert hash
	var nonce [32]byte
	copy(nonce[:], serverHelloBuf[1:33])
	var wrongHash [48]byte // all zeros — wrong
	agentHello := protocol.EncodeAgentHello(nonce, wrongHash)
	_, err = clientConn.Write(agentHello)
	require.NoError(t, err)

	res := <-ch
	assert.Error(t, res.err)
	assert.True(t, errors.Is(res.err, ErrHandshakeFailed))
}

func TestHandshaker_EmptyStream(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	h := NewHandshaker(cm)

	// Create pipe and close client side immediately
	serverConn, clientConn := net.Pipe()
	clientConn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = h.PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
	serverConn.Close()
}

func TestHandshaker_Timeout(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	h := NewHandshaker(cm)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Use a very short timeout — client never responds
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Read the ServerHello on client side so write doesn't block
	go func() {
		buf := make([]byte, 81)
		io.ReadFull(clientConn, buf)
		// Don't respond — let it time out
	}()

	_, err = h.PerformHandshake(ctx, serverConn, nil)
	assert.Error(t, err)
}

func TestHandshaker_ServerHelloContainsValidCACertHash(t *testing.T) {
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)

	h := NewHandshaker(cm)

	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close(); clientConn.Close() })

	// Start handshake in goroutine (will block waiting for AgentHello)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		h.PerformHandshake(ctx, serverConn, nil)
	}()

	// Read ServerHello and verify cert hash matches the CA
	buf := make([]byte, 81)
	_, err = io.ReadFull(clientConn, buf)
	require.NoError(t, err)

	assert.Equal(t, byte(protocol.MsgServerHello), buf[0])

	var receivedHash [48]byte
	copy(receivedHash[:], buf[33:81])

	expectedHash := sha512.Sum384(cm.CACert().Raw)
	assert.Equal(t, expectedHash, receivedHash)

	// Clean up by sending something to unblock the goroutine
	clientConn.Write(bytes.Repeat([]byte{0}, 81))
}
