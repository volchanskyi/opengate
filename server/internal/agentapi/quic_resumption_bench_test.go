package agentapi

import (
	"crypto/tls"
	"testing"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// W3 measurement — per-reconnect handshake cost, cold mTLS vs 1-RTT resumed.
// The delta between the two benchmarks is the saving the W3 decision quantifies.
// Shared harness (server, cert config, dial/ticket helpers) lives in
// quic_resumption_test.go.

func benchmarkQUICHandshake(b *testing.B, resume bool) {
	mgr, err := cert.NewManager(b.TempDir())
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}
	srv := startResumeTestServer(b, mgr, false)

	var cache tls.ClientSessionCache
	if resume {
		sc := newSignalingCache()
		cache = sc
		_ = dialRoundTrip(b, srv.addr, agentResumeTLSConfig(b, mgr, sc)) // prime the ticket
		waitForTicket(b, sc)
	}
	clientCfg := agentResumeTLSConfig(b, mgr, cache)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if st := dialRoundTrip(b, srv.addr, clientCfg); resume && !st.DidResume {
			b.Fatalf("expected resumed handshake, got full")
		}
	}
}

// BenchmarkQUICHandshake_Cold measures a full mTLS reconnect.
func BenchmarkQUICHandshake_Cold(b *testing.B) { benchmarkQUICHandshake(b, false) }

// BenchmarkQUICHandshake_Resumed measures a 1-RTT resumed reconnect.
func BenchmarkQUICHandshake_Resumed(b *testing.B) { benchmarkQUICHandshake(b, true) }
