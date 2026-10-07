package agentapi

import (
	"crypto/tls"
	"testing"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

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
		_ = dialRoundTrip(b, srv.addr, agentResumeTLSConfig(b, mgr, sc))
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

func BenchmarkQUICHandshake_Cold(b *testing.B) { benchmarkQUICHandshake(b, false) }

func BenchmarkQUICHandshake_Resumed(b *testing.B) { benchmarkQUICHandshake(b, true) }
