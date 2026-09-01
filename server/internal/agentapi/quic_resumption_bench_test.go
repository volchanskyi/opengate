package agentapi

import (
	"crypto/tls"
	"testing"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

// Per-reconnect handshake cost, cold mutual TLS against a 1-RTT resumed
// session. The gap between the two is what a reconnect saves by resuming.
// The shared harness — server, certificate configuration, dial and ticket
// helpers — lives in quic_resumption_test.go.

func benchmarkQUICHandshake(b *testing.B, resume bool) {
	mgr, err := cert.NewManager(b.TempDir())
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}
	srv := startResumeTestServer(b, mgr)

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
