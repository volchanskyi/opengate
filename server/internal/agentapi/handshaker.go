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
}

// NewHandshaker creates a new Handshaker.
func NewHandshaker(cm *cert.Manager) *Handshaker {
	return &Handshaker{cert: cm}
}

// PerformHandshake drives the ServerHello/AgentHello exchange over a stream.
// peerCerts contains the DER-encoded certificates presented by the TLS peer.
func (h *Handshaker) PerformHandshake(ctx context.Context, stream io.ReadWriter, peerCerts [][]byte) (*HandshakeResult, error) {
	// Apply deadline from context if the stream supports it.
	if deadline, ok := ctx.Deadline(); ok {
		if conn, ok := stream.(net.Conn); ok {
			conn.SetDeadline(deadline)
		}
	}

	// Step 1: Generate nonce and compute CA cert hash.
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	caCertHash := sha512.Sum384(h.cert.CACert().Raw)

	// Step 2: Send ServerHello.
	serverHello := protocol.EncodeServerHello(nonce, caCertHash)
	if _, err := stream.Write(serverHello); err != nil {
		return nil, fmt.Errorf("write ServerHello: %w", err)
	}

	// Step 3: Read AgentHello (81 bytes).
	agentHelloBuf := make([]byte, 81)
	if _, err := io.ReadFull(stream, agentHelloBuf); err != nil {
		return nil, fmt.Errorf("read AgentHello: %w", err)
	}

	// Step 4: Validate type byte.
	if agentHelloBuf[0] != protocol.MsgAgentHello {
		return nil, fmt.Errorf("%w: expected AgentHello (0x%02x), got 0x%02x",
			ErrHandshakeFailed, protocol.MsgAgentHello, agentHelloBuf[0])
	}

	// Step 5: Extract agent cert hash from the message.
	var agentCertHash [48]byte
	copy(agentCertHash[:], agentHelloBuf[33:81])

	// Step 6: Verify agent cert hash matches the presented peer certificate.
	if len(peerCerts) == 0 {
		return nil, fmt.Errorf("%w: no peer certificates presented", ErrHandshakeFailed)
	}

	peerCertDER := peerCerts[0]
	expectedHash := sha512.Sum384(peerCertDER)
	if agentCertHash != expectedHash {
		return nil, fmt.Errorf("%w: agent cert hash mismatch", ErrHandshakeFailed)
	}

	// Step 7: Extract DeviceID from the peer certificate's CN.
	peerCert, err := x509.ParseCertificate(peerCertDER)
	if err != nil {
		return nil, fmt.Errorf("%w: parse peer cert: %v", ErrHandshakeFailed, err)
	}

	deviceID, err := uuid.Parse(peerCert.Subject.CommonName)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid device ID in cert CN %q: %v",
			ErrHandshakeFailed, peerCert.Subject.CommonName, err)
	}

	return &HandshakeResult{
		DeviceID:     deviceID,
		AgentCertDER: peerCertDER,
		Skipped:      false,
	}, nil
}
