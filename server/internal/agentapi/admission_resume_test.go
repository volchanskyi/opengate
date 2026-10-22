package agentapi

import (
	"crypto/sha512"
	"crypto/tls"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

func TestServerHelloCarriesIssuerHash(t *testing.T) {
	env := newCertEnv(t, "")
	_, machine := env.machine(t)
	m := env.dial(t, machine)
	issuer := sha512.Sum384(env.cm.CACert().Raw)
	assert.Equal(t, issuer[:], m.hello[33:81])
}

func TestVerifyConnectionRunsOnResumedHandshake(t *testing.T) {
	mgr, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	var verified atomic.Int32
	srv := startResumeTestServerWith(t, mgr, func(cfg *tls.Config) {
		inner := cfg.VerifyConnection
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			verified.Add(1)
			return inner(cs)
		}
	})
	cache := newSignalingCache()
	cfg := agentResumeTLSConfig(t, mgr, cache)

	dialRoundTrip(t, srv.addr, cfg)
	waitForTicket(t, cache)
	state := dialRoundTrip(t, srv.addr, cfg)

	require.True(t, state.DidResume, "the second dial resumed")
	assert.Equal(t, int32(2), verified.Load(), "the resumed handshake was classified too")
}

func TestTicketOfExpiredCertNeverResumes(t *testing.T) {
	clock := &serverClock{t: time.Now()}
	mgr, err := cert.NewManager(t.TempDir(), cert.WithClock(clock.Now))
	require.NoError(t, err)
	srv := startResumeTestServer(t, mgr)
	cache := newSignalingCache()
	cfg := agentResumeTLSConfig(t, mgr, cache)
	cfg.Time = clock.Now

	dialRoundTrip(t, srv.addr, cfg)
	waitForTicket(t, cache)
	clock.Advance(leafLife + day)
	state := dialRoundTrip(t, srv.addr, cfg)

	assert.False(t, state.DidResume, "a ticket naming an expired certificate is refused for resumption")
}
