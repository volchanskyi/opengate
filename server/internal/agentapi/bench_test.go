package agentapi

import (
	"bytes"
	"context"
	"crypto/sha512"
	"testing"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// scriptedStream replays a fixed request and swallows the reply. The handshake
// is a strict request/response over a stream, so the agent's side of it is a
// byte slice known before the run starts — replaying it needs no peer, and
// therefore no goroutine, no pipe and no deadline to bound one.
//
// That is what keeps this benchmark's figure about the handshake. A net.Pipe is
// synchronous and unbuffered, so every read and write is a scheduler handoff,
// and those handoffs, not the handshake, set both the level and the variance of
// what gets published. Replaying from a bytes.Reader cannot block, so the loop
// body is the server's own work and nothing else.
type scriptedStream struct {
	req   bytes.Reader
	reply bytes.Buffer
}

func (s *scriptedStream) Read(p []byte) (int, error)  { return s.req.Read(p) }
func (s *scriptedStream) Write(p []byte) (int, error) { return s.reply.Write(p) }

// reset rewinds the stream so the next iteration reads the same request.
func (s *scriptedStream) reset(req []byte) {
	s.req.Reset(req)
	s.reply.Reset()
}

// benchmarkHandshake builds one handshaker and the agent certificate its peer
// presents, derives the agent's request from that same fixture, and replays it.
// All of that is per-run setup and stays outside the timed loop.
//
// The path taken is asserted every iteration, so a change that silently reroutes
// a cold start onto the fast path cannot pass as a speed-up.
func benchmarkHandshake(b *testing.B, request func(h *Handshaker, peerCertDER []byte) []byte, wantSkipped bool) {
	b.Helper()

	mgr, err := cert.NewManager(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	agentCert, err := mgr.SignAgent(uuid.New().String(), "test-host")
	if err != nil {
		b.Fatal(err)
	}
	h := NewHandshaker(mgr)
	peerCertDER := agentCert.Certificate[0]
	peerCerts := [][]byte{peerCertDER}
	req := request(h, peerCertDER)

	var stream scriptedStream
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stream.reset(req)
		res, err := h.PerformHandshake(ctx, &stream, peerCerts)
		if err != nil {
			b.Fatal(err)
		}
		if res.Skipped != wantSkipped {
			b.Fatalf("Skipped = %v, want %v", res.Skipped, wantSkipped)
		}
	}
}

// BenchmarkHandshaker_PerformHandshake measures a cold start: the 0x11
// AgentHello path, which binds the advertised certificate hash to the TLS peer
// certificate and replies with a ServerHello. This is the per-connection cost
// the agent-facing path is sized on when a fleet arrives holding no cached CA
// hash.
func BenchmarkHandshaker_PerformHandshake(b *testing.B) {
	benchmarkHandshake(b, func(_ *Handshaker, peerCertDER []byte) []byte {
		var nonce [32]byte
		return protocol.EncodeAgentHello(nonce, sha512.Sum384(peerCertDER))
	}, false)
}

// BenchmarkHandshaker_PerformHandshake_FastPath measures a reconnect: the 0x14
// SkipAuth path, which checks the agent's cached CA hash against the current CA
// and returns without a reply. A fleet-wide reconnect storm arrives on this
// path, so it — not the cold start — is the cost that storm is sized on.
func BenchmarkHandshaker_PerformHandshake_FastPath(b *testing.B) {
	benchmarkHandshake(b, func(h *Handshaker, _ []byte) []byte {
		return protocol.EncodeSkipAuth(h.caCertHash)
	}, true)
}
