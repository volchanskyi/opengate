package agentapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// Whether a reconnecting machine resumed its TLS session is a saving the fleet
// either earns or does not, and the server is the only side that can say. The
// machine's own transport reports no resumption result, and a ticket it presents
// may still be declined here — so the count is taken from this listener's own
// connection state, against a real QUIC handshake rather than a hand-built one.

// TestTheServerCountsWhetherATLSSessionResumed drives one machine through a
// cold connection and a reconnect on the same certificate and session cache,
// and asserts each lands on its own series.
func TestTheServerCountsWhetherATLSSessionResumed(t *testing.T) {
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	env := newAcceptEnvWithMetrics(t, m)

	// One machine: one certificate and one session cache, held across both
	// attempts, exactly as a running agent holds its quinn configuration.
	deviceID := uuid.New()
	tlsCert, err := env.srv.cert.SignAgent(deviceID.String(), "resumption-test")
	require.NoError(t, err)
	cache := newSignalingCache()
	tlsCfg := env.srv.cert.AgentTLSConfig(tlsCert)
	tlsCfg.ClientSessionCache = cache

	resumed := func(v string) float64 {
		return testutil.ToFloat64(m.AgentTLSHandshakesTotal.WithLabelValues(v))
	}

	// The first connection has nothing to resume from.
	firstConn, firstStream := env.dialWith(t, tlsCfg)
	readServerHello(t, firstStream)
	assert.InDelta(t, 1, resumed("false"), 0, "a first connection is a full handshake")
	assert.InDelta(t, 0, resumed("true"), 0, "with nothing yet to resume from")

	// The machine comes back on the ticket the server issued it.
	waitForTicket(t, cache)
	_ = firstConn.CloseWithError(0, "reconnecting")

	_, secondStream := env.dialWith(t, tlsCfg)
	readServerHello(t, secondStream)
	assert.InDelta(t, 1, resumed("true"), 0,
		"the reconnect skipped the asymmetric handshake, and the server says so")
	assert.InDelta(t, 1, resumed("false"), 0,
		"and it is not counted as a full handshake as well")
}

// TestAServerWithoutMetricsStillAcceptsMachines pins the nil-metrics
// convention this package runs on: instrumentation is optional, and a server
// built without it accepts connections rather than panicking on the first one.
func TestAServerWithoutMetricsStillAcceptsMachines(t *testing.T) {
	env := newAcceptEnv(t)
	require.Nil(t, env.srv.metrics)

	_, stream := env.connect(t, uuid.New())
	register(t, stream, "unmetered")
	waitForCount(t, env.srv, 1)
}
