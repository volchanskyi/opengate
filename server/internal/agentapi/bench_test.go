package agentapi

import (
	"bytes"
	"compress/flate"
	"context"
	"crypto/rand"
	"crypto/sha512"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
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

// Admitting one alert: the checks that stand between a machine and a
// technician's queue.
//
// An alert is the only thing on the control channel that carries the detail
// behind a signal — there is no high-resolution history to go back to and no
// path for asking the machine later — so every check here runs on the read
// loop, which also carries this machine's remote-management paths. A storm is
// exactly when that channel matters most, which is why this figure is worth
// keeping: it is what one machine at its hourly ceiling costs the loop.
//
// The store write is deliberately outside it. That happens on a bounded slot
// goroutine, and folding its cost in here would measure the database rather
// than the admission this benchmark is about.
func BenchmarkAdmitAlert(b *testing.B) {
	conn := benchAlertConn(b)
	msg := benchAlert(b)

	b.ReportAllocs()
	for b.Loop() {
		if _, ok := conn.validatedAlert(msg); !ok {
			b.Fatal("the alert under benchmark must be one the product accepts")
		}
	}
}

// The same, for an alert carrying the largest evidence the contract allows.
// Evidence is read back before it is believed — a blob that does not decompress
// is worse than none, because it reads as evidence that exists — and that read
// is the part that scales with what the machine attached.
func BenchmarkAdmitAlertWithFullEvidence(b *testing.B) {
	conn := benchAlertConn(b)
	msg := benchAlert(b)
	msg.Evidence = benchEvidence(b, protocol.MaxEvidenceBytes/2)

	b.ReportAllocs()
	for b.Loop() {
		if _, ok := conn.validatedAlert(msg); !ok {
			b.Fatal("the alert under benchmark must be one the product accepts")
		}
	}
}

// benchAlertConn is a connection wired the way the product wires one for
// alerts: the shipped catalogue, so a rule is actually looked up rather than
// waved through.
func benchAlertConn(b *testing.B) *AgentConn {
	b.Helper()
	catalogue, err := rules.Embedded()
	if err != nil {
		b.Fatal(err)
	}
	return &AgentConn{
		DeviceID:    uuid.New(),
		ruleCatalog: catalogue,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// benchAlert is one well-formed alert, built once outside the timed loop.
func benchAlert(b *testing.B) *protocol.ControlMessage {
	b.Helper()
	severity := protocol.AlertSeverityCritical
	backfilled := false
	value := 91.4
	end := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
	return &protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        "disk-critical",
		RuleVersion:   1,
		Severity:      &severity,
		Metric:        "disk.used_percent",
		Value:         &value,
		WindowStartTS: end.Add(-5 * time.Minute).Unix(),
		WindowEndTS:   end.Unix(),
		ObservedTS:    end.Unix(),
		Backfilled:    &backfilled,
		EvidenceCodec: protocol.EvidenceCodec,
		Evidence:      benchEvidence(b, 512),
	}
}

// benchEvidence packs a blob under the codec the contract names, so the read
// back under benchmark is the real one.
func benchEvidence(b *testing.B, size int) []byte {
	b.Helper()
	var out bytes.Buffer
	writer, err := flate.NewWriter(&out, flate.BestSpeed)
	if err != nil {
		b.Fatal(err)
	}
	// Incompressible, so the blob under benchmark is the size it says it is
	// rather than a few bytes of repeated padding.
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		b.Fatal(err)
	}
	if _, err := writer.Write(raw); err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
	return out.Bytes()
}
