package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A sample of the server's own metrics page, trimmed to the two families this
// reads plus a neighbour, so a parser that matches too loosely is caught.
const sampleMetricsPage = `# HELP opengate_agent_registration_duration_seconds Time from an accepted AgentRegister frame to the device row being written and online, by outcome.
# TYPE opengate_agent_registration_duration_seconds histogram
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="0.001"} 0
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="0.005"} 10
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="0.01"} 60
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="0.025"} 90
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="0.05"} 98
opengate_agent_registration_duration_seconds_bucket{result="accepted",le="+Inf"} 100
opengate_agent_registration_duration_seconds_sum{result="accepted"} 1.4
opengate_agent_registration_duration_seconds_count{result="accepted"} 100
opengate_agent_registration_duration_seconds_bucket{result="rejected",le="+Inf"} 3
opengate_agent_registration_duration_seconds_sum{result="rejected"} 0.03
opengate_agent_registration_duration_seconds_count{result="rejected"} 3
# HELP opengate_agent_registrations_total Total agent registrations the server completed, by outcome.
# TYPE opengate_agent_registrations_total counter
opengate_agent_registrations_total{result="accepted"} 100
opengate_agent_registrations_total{result="rejected"} 3
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
