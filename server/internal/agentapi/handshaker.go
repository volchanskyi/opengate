package agentapi

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"fmt"
	"io"
	"net"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// HandshakeResult contains the authenticated identity from a successful handshake.
type HandshakeResult struct {
	// DeviceID is the agent's unique device identifier, extracted from the cert CN.
	DeviceID protocol.DeviceID
	// AgentCertDER is the raw DER bytes of the agent's certificate.
	AgentCertDER []byte
	// Skipped is true if the agent used SkipAuth with a valid cached hash.
	Skipped bool
}

// Handshaker performs the binary mTLS handshake with a newly connected agent.
type Handshaker struct {
	cert *cert.Manager
	// rand is the randomness source for the ServerHello nonce; overridable in
	// tests to exercise the generation-failure path.
	rand io.Reader
}

// NewHandshaker creates a new Handshaker.
func NewHandshaker(cm *cert.Manager) *Handshaker {
	return &Handshaker{cert: cm, rand: rand.Reader}
}

// PerformHandshake authenticates a newly connected agent. The agent opens the
// stream and writes first (RFC 9000 stream-discovery: the opener must write
// before the peer's accept/read can return), so the server reads the agent's
// first message and branches on its type:
//
//   - 0x11 AgentHello → full handshake: bind the advertised cert hash to the
//     TLS peer cert, then reply with ServerHello.
//   - 0x14 SkipAuth   → fast-path reconnect: verify the cached CA hash is
//     current and skip the ServerHello/AgentHello round-trip (no reply).
//
// mTLS is the authenticator on both paths; the message exchange binds identity
// and advertises the CA hash. peerCerts holds the DER certs the TLS peer
// presented.
func (h *Handshaker) PerformHandshake(ctx context.Context, stream io.ReadWriter, peerCerts [][]byte) (*HandshakeResult, error) {
	// Apply deadline from context if the stream supports it.
	if deadline, ok := ctx.Deadline(); ok {
		if conn, ok := stream.(net.Conn); ok {
			_ = conn.SetDeadline(deadline)
		}
	}

	// Read the 1-byte message type to choose the full or fast path.
	var typeByte [1]byte
	if _, err := io.ReadFull(stream, typeByte[:]); err != nil {
		return nil, fmt.Errorf("read handshake type: %w", err)
	}

	switch typeByte[0] {
	case protocol.MsgAgentHello:
		return h.fullHandshake(stream, peerCerts)
	case protocol.MsgSkipAuth:
		return h.fastHandshake(stream, peerCerts)
	default:
		return nil, fmt.Errorf("%w: expected AgentHello (0x%02x) or SkipAuth (0x%02x), got 0x%02x",
			ErrHandshakeFailed, protocol.MsgAgentHello, protocol.MsgSkipAuth, typeByte[0])
	}
}

// fullHandshake completes a cold-start handshake: read the rest of AgentHello,
// bind the advertised cert hash to the TLS peer cert, and reply ServerHello.
// The 1-byte type has already been consumed by the caller.
func (h *Handshaker) fullHandshake(stream io.ReadWriter, peerCerts [][]byte) (*HandshakeResult, error) {
	// AgentHello body: 32-byte nonce + 48-byte agent cert hash.
	body := make([]byte, 80)
	if _, err := io.ReadFull(stream, body); err != nil {
		return nil, fmt.Errorf("read AgentHello: %w", err)
	}
	var agentCertHash [48]byte
	copy(agentCertHash[:], body[32:80])

	deviceID, peerCertDER, err := identityFromPeer(peerCerts)
	if err != nil {
		return nil, err
	}
	if agentCertHash != sha512.Sum384(peerCertDER) {
		return nil, fmt.Errorf("%w: agent cert hash mismatch", ErrHandshakeFailed)
	}

	if err := h.writeServerHello(stream); err != nil {
		return nil, err
	}

	return &HandshakeResult{DeviceID: deviceID, AgentCertDER: peerCertDER, Skipped: false}, nil
}

// fastHandshake completes a 0x14 reconnect: read the cached CA hash, verify it
// matches the current CA cert, and skip the ServerHello round-trip. A stale
// hash is rejected so the agent falls back to the full handshake. The 1-byte
// type has already been consumed by the caller.
func (h *Handshaker) fastHandshake(stream io.Reader, peerCerts [][]byte) (*HandshakeResult, error) {
	var cachedHash [48]byte
	if _, err := io.ReadFull(stream, cachedHash[:]); err != nil {
		return nil, fmt.Errorf("read SkipAuth: %w", err)
	}
	if cachedHash != sha512.Sum384(h.cert.CACert().Raw) {
		return nil, fmt.Errorf("%w: stale CA cert hash on fast path", ErrHandshakeFailed)
	}

	// mTLS already authenticated the agent; identity still comes from the cert.
	deviceID, peerCertDER, err := identityFromPeer(peerCerts)
	if err != nil {
		return nil, err
	}

	// No ServerHello reply on the fast path — the agent proceeds optimistically.
	return &HandshakeResult{DeviceID: deviceID, AgentCertDER: peerCertDER, Skipped: true}, nil
}

// writeServerHello generates a nonce and writes the ServerHello reply carrying
// the current CA cert hash.
func (h *Handshaker) writeServerHello(stream io.Writer) error {
	var nonce [32]byte
	if _, err := io.ReadFull(h.rand, nonce[:]); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	caCertHash := sha512.Sum384(h.cert.CACert().Raw)
	if _, err := stream.Write(protocol.EncodeServerHello(nonce, caCertHash)); err != nil {
		return fmt.Errorf("write ServerHello: %w", err)
	}
	return nil
}

// identityFromPeer extracts the agent's DeviceID from the TLS peer certificate
// CN — the mTLS-authenticated identity shared by both handshake paths.
func identityFromPeer(peerCerts [][]byte) (protocol.DeviceID, []byte, error) {
	if len(peerCerts) == 0 {
		return uuid.Nil, nil, fmt.Errorf("%w: no peer certificates presented", ErrHandshakeFailed)
	}
	peerCertDER := peerCerts[0]
	peerCert, err := x509.ParseCertificate(peerCertDER)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: parse peer cert: %v", ErrHandshakeFailed, err)
	}
	deviceID, err := uuid.Parse(peerCert.Subject.CommonName)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("%w: invalid device ID in cert CN %q: %v",
			ErrHandshakeFailed, peerCert.Subject.CommonName, err)
	}
	return deviceID, peerCertDER, nil
}
