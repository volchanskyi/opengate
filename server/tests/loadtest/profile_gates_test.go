package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func profileWith(gates, ungated string) string {
	document := `
schema_version: 1
name: normal
family: normal
environment: staging
fixture: small
phases:
  - name: steady
    duration: 2m
    connected_agents: 500
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
gates:
` + gates
	if ungated != "" {
		document += "ungated:\n" + ungated
	}
	return document
}

func gate(series, metric, direction string, value float64, failing bool) string {
	return strings.Join([]string{
		"  - series: " + series,
		"    metric: " + metric,
		"    " + direction + ": " + strconv.FormatFloat(value, 'f', -1, 64),
		"    blocking: " + strconv.FormatBool(failing),
		"",
	}, "\n")
}

func exemption(series, metric, reason string) string {
	return strings.Join([]string{
		"  - series: " + series,
		"    metric: " + metric,
		"    reason: " + reason,
		"",
	}, "\n")
}

func apiLatency(failing bool, value float64) string {
	return gate("k6/api-baseline/http", "latency_p95_ms", "max", value, failing)
}

func TestAMeasurementMayCarryOneFailingLimitAndAnyNumberOfMarks(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200)+apiLatency(false, 100), "")))
	require.NoError(t, err)
	require.Len(t, p.Gates, 2)

	assert.True(t, p.Gates[0].Blocking, "the collapse limit fails the night")
	assert.False(t, p.Gates[1].Blocking, "the target is watched rather than enforced")
}

func TestTwoFailingLimitsOnOneMeasurementAreRefused(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(
		apiLatency(true, 200)+apiLatency(false, 100)+apiLatency(true, 150), "")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than one gate that fails the run")
	assert.Contains(t, err.Error(), "k6/api-baseline/http")
}

func TestALimitAboveAndALimitBelowAreNotADuplicate(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(
		gate("quic/quic-agents/aggregate", "rps", "min", 10, true)+
			gate("quic/quic-agents/aggregate", "error_rate", "max", 0, true), "")))

	require.NoError(t, err)
}

func TestAMeasurementLeftUnlimitedIsDeclaredWithItsReason(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "rps", "the rate is the pacing this scenario asks for"))))
	require.NoError(t, err)

	require.Len(t, p.Ungated, 1)
	assert.Equal(t, "k6/api-baseline/http", p.Ungated[0].Series)
	assert.Equal(t, "rps", p.Ungated[0].Metric)
	assert.NotEmpty(t, p.Ungated[0].Reason)
}

func TestAnUnlimitedMeasurementWithNoReasonIsRefused(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		"  - series: k6/api-baseline/http\n    metric: rps\n")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason")
}

func TestAMeasurementCannotBeBothLimitedAndDeclaredUnlimited(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "latency_p95_ms", "it is limited above, which is the contradiction"))))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "both limited and declared unlimited")
}

func TestAProfileReportsWhichMeasurementsItHasDecided(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "rps", "the rate is the pacing this scenario asks for"))))
	require.NoError(t, err)

	decided := p.DecidedMeasurements()
	assert.True(t, decided["k6/api-baseline/http|latency_p95_ms"], "a limited measurement is decided")
	assert.True(t, decided["k6/api-baseline/http|rps"], "a deliberately unlimited one is decided too")
	assert.False(t, decided["k6/api-baseline/http|error_rate"], "one nobody has ruled on is not")
}
