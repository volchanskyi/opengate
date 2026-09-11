package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// A sample of the server's own metrics page, trimmed to the two families this
// reads plus a neighbour, so a parser that matches too loosely is caught.
const sampleMetricsPage = `# HELP opengate_agent_registration_duration_seconds Time from an accepted AgentRegister frame to the device row being written and online, by outcome.
# TYPE opengate_agent_registration_duration_seconds histogram
opengate_agent_registration_duration_seconds_bucket{result="ok",le="0.001"} 0
opengate_agent_registration_duration_seconds_bucket{result="ok",le="0.005"} 10
opengate_agent_registration_duration_seconds_bucket{result="ok",le="0.01"} 60
opengate_agent_registration_duration_seconds_bucket{result="ok",le="0.025"} 90
opengate_agent_registration_duration_seconds_bucket{result="ok",le="0.05"} 98
opengate_agent_registration_duration_seconds_bucket{result="ok",le="+Inf"} 100
opengate_agent_registration_duration_seconds_sum{result="ok"} 1.4
opengate_agent_registration_duration_seconds_count{result="ok"} 100
opengate_agent_registration_duration_seconds_bucket{result="error",le="+Inf"} 3
opengate_agent_registration_duration_seconds_sum{result="error"} 0.03
opengate_agent_registration_duration_seconds_count{result="error"} 3
# HELP opengate_agent_registrations_total Total agent registrations the server completed, by outcome.
# TYPE opengate_agent_registrations_total counter
opengate_agent_registrations_total{result="ok"} 100
opengate_agent_registrations_total{result="error"} 3
# HELP opengate_db_pool_connections Database connection-pool occupancy by state.
# TYPE opengate_db_pool_connections gauge
opengate_db_pool_connections{state="open"} 7
opengate_db_pool_connections{state="in_use"} 2
opengate_db_pool_connections{state="idle"} 5
opengate_db_pool_connections{state="max_open"} 25
`

func TestServerRegistrationCountsEveryOutcomeSeparately(t *testing.T) {
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)

	assert.Equal(t, int64(100), reading.Accepted)
	// A refused registration is the server working — a tombstoned machine, a
	// spent token — so it is counted apart from the accepted ones rather than
	// folded into a single rate that hides both.
	assert.Equal(t, int64(3), reading.Rejected)
}

func TestServerRegistrationReportsTheTailNotJustTheAverage(t *testing.T) {
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)

	// Ninety of the hundred landed at or below 25ms and ninety-eight at or below
	// 50ms, so the ninety-fifth sits inside that last stretch. The average is
	// 14ms and says nothing at all about those ten.
	assert.InDelta(t, 14.0, reading.MeanMs(), 0.5)
	p95 := reading.QuantileMs(0.95)
	assert.Greater(t, p95, 25.0)
	assert.LessOrEqual(t, p95, 50.0)
}

func TestServerRegistrationQuantileHandlesTheEnds(t *testing.T) {
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, reading.QuantileMs(0.5), 5.0)
	assert.LessOrEqual(t, reading.QuantileMs(0.5), 10.0)
	// Everything is inside the last finite bucket, so the top of the range is
	// reported rather than an infinity nothing can be compared against.
	assert.LessOrEqual(t, reading.QuantileMs(0.999), 50.0)
}

func TestServerRegistrationOnAnEmptyPageIsNotAMeasurement(t *testing.T) {
	reading, err := ParseServerRegistration("# nothing here\n")
	require.NoError(t, err)

	assert.Zero(t, reading.Accepted)
	// A run that measured nothing must not read as a run that measured zero
	// milliseconds, which would be the fastest night ever recorded.
	assert.False(t, reading.Measured())
}

func TestServerRegistrationReadsThePoolBesideIt(t *testing.T) {
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)

	// A registration queued behind a connection and one executing slowly are
	// the same latency until the pool says otherwise.
	assert.Equal(t, 7.0, reading.PoolOpen)
	assert.Equal(t, 2.0, reading.PoolInUse)
	assert.Equal(t, 25.0, reading.PoolMaxOpen)
}

func TestFetchServerRegistrationReadsTheRunningServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/metrics", r.URL.Path)
		_, _ = w.Write([]byte(sampleMetricsPage))
	}))
	defer server.Close()

	reading, err := FetchServerRegistration(server.URL)
	require.NoError(t, err)
	assert.True(t, reading.Measured())
	assert.Equal(t, int64(100), reading.Accepted)
}

func TestFetchServerRegistrationReportsAnUnreachableServer(t *testing.T) {
	_, err := FetchServerRegistration("http://127.0.0.1:1")
	require.Error(t, err)
}

func TestFetchServerRegistrationReportsARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := FetchServerRegistration(server.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "403")
}

// The page the parser reads is the page the server writes.
//
// Every case above this one reads a page written by hand to match the parser,
// and the parser was looking for an outcome label the server has never
// published. So the reading came back with nothing accepted on every run that
// ever took it, the registration line was absent from every results block, and
// the three limits held against it were limits on a measurement nothing
// produced. It is the same defect the reading was built to close, one layer
// further out: a number taken from the wrong place cannot move.
//
// So the page below is rendered from the server's own instrument rather than
// written out here, and a label renamed on either side fails this.
func TestRegistrationIsReadOffThePageTheServerActuallyWrites(t *testing.T) {
	registry := prometheus.NewRegistry()
	instrument := appmetrics.NewMetrics(registry)
	instrument.ObserveAgentRegistration(appmetrics.RegistrationOK, 7*time.Millisecond)
	instrument.ObserveAgentRegistration(appmetrics.RegistrationOK, 9*time.Millisecond)
	instrument.ObserveAgentRegistration(appmetrics.RegistrationError, time.Millisecond)

	reading, err := ParseServerRegistration(renderExposition(t, registry))
	require.NoError(t, err)

	assert.True(t, reading.Measured(), "the server saw two registrations and the reading says so")
	assert.Equal(t, int64(2), reading.Accepted)
	assert.Equal(t, int64(1), reading.Rejected)
	assert.InDelta(t, 8.0, reading.MeanMs(), 0.001)
}

// renderExposition writes a registry out in the text form the harness fetches.
func renderExposition(t *testing.T, registry *prometheus.Registry) string {
	t.Helper()

	families, err := registry.Gather()
	require.NoError(t, err)

	var page strings.Builder
	encoder := expfmt.NewEncoder(&page, expfmt.NewFormat(expfmt.TypeTextPlain))
	for _, family := range families {
		require.NoError(t, encoder.Encode(family))
	}
	return page.String()
}
