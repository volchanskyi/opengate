package agentapi

import (
	"context"
	"crypto/sha512"
	"net"
	"testing"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func BenchmarkHandshaker_PerformHandshake(b *testing.B) {
	mgr, err := cert.NewManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	deviceID := uuid.New()
	agentCert, err := mgr.SignAgent(deviceID.String(), "test-host")
	if err != nil {
		b.Fatal(err)
	}
	peerCertDER := agentCert.Certificate[0]

	h := NewHandshaker(mgr)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		serverConn, clientConn := net.Pipe()

		// Run client side in goroutine. The agent opens + writes first, so
		// send AgentHello before reading the server's reply.
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer clientConn.Close()

			agentCertHash := sha512.Sum384(peerCertDER)
			var nonce [32]byte
			agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
			if _, err := clientConn.Write(agentHello); err != nil {
				return
			}

			// Read ServerHello (81 bytes)
			buf := make([]byte, 81)
			clientConn.Read(buf)
		}()

		_, err := h.PerformHandshake(context.Background(), serverConn, [][]byte{peerCertDER})
		serverConn.Close()
		<-done

		if err != nil {
			b.Fatal(err)
		}
	}
}
