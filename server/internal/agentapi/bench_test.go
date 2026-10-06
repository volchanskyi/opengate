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

// scriptedStream replays a fixed request from a bytes.Reader and discards the reply, so the
// timed loop needs no peer goroutine or pipe.
type scriptedStream struct {
	req   bytes.Reader
	reply bytes.Buffer
}

func (s *scriptedStream) Read(p []byte) (int, error)  { return s.req.Read(p) }
func (s *scriptedStream) Write(p []byte) (int, error) { return s.reply.Write(p) }

func (s *scriptedStream) reset(req []byte) {
	s.req.Reset(req)
	s.reply.Reset()
}

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

func BenchmarkHandshaker_PerformHandshake(b *testing.B) {
	benchmarkHandshake(b, func(_ *Handshaker, peerCertDER []byte) []byte {
		var nonce [32]byte
		return protocol.EncodeAgentHello(nonce, sha512.Sum384(peerCertDER))
	}, false)
}

func BenchmarkHandshaker_PerformHandshake_FastPath(b *testing.B) {
	benchmarkHandshake(b, func(h *Handshaker, _ []byte) []byte {
		return protocol.EncodeSkipAuth(h.caCertHash)
	}, true)
}

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

// benchAlertConn builds a connection wired with the shipped rule catalogue.
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

func benchEvidence(b *testing.B, size int) []byte {
	b.Helper()
	var out bytes.Buffer
	writer, err := flate.NewWriter(&out, flate.BestSpeed)
	if err != nil {
		b.Fatal(err)
	}
	// Random bytes keep the packed blob at the requested size.
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
