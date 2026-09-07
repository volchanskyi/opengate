package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// One measurement, one number that fails a night, and every measurement
// accounted for.
//
// The numbers deciding whether a night was acceptable lived in two places. A
// script kept a set of "something has collapsed" limits, and the profile kept
// its own — with different values for the same measurement: 200 in the script
// and 100 in the profile, one enforced and one read by nothing. Turning the
// profile's on without settling that gives one measurement two enforced numbers
// in two files, and an edit to either does not do what it says.
//
// The settlement is that the profile is the only home. A measurement may carry
// one limit that fails the night and any number of marks that only report — the
// two are different statements, and collapsing them loses whichever is dropped.

// profileWith is a valid profile carrying whatever limits and exemptions a case
// wants to try. Everything above them is the same in every case, so it is
// written once and the case reads as the difference it is about.
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

// gate renders one limit. Direction is "max" or "min"; failing says whether a
// breach fails the night or only reports.
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

// The limit and the mark this whole arrangement was settled over: the same
// measurement, one number that fails the night and one that watches.
func apiLatency(failing bool, value float64) string {
	return gate("k6/api-baseline/http", "latency_p95_ms", "max", value, failing)
}

// A measurement carries at most one number that fails a night. Marks that only
// report may sit beside it, because a target being watched and a limit being
// enforced are different statements about the same measurement.
func TestAMeasurementMayCarryOneFailingLimitAndAnyNumberOfMarks(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200)+apiLatency(false, 100), "")))
	require.NoError(t, err)
	require.Len(t, p.Gates, 2)

	assert.True(t, p.Gates[0].Blocking, "the collapse limit fails the night")
	assert.False(t, p.Gates[1].Blocking, "the target is watched rather than enforced")
}

// Two numbers that both fail a night, on one measurement, is the defect this
// whole settlement exists to close — arrived at inside one file this time.
func TestTwoFailingLimitsOnOneMeasurementAreRefused(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(
		apiLatency(true, 200)+apiLatency(false, 100)+apiLatency(true, 150), "")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than one gate that fails the run")
	assert.Contains(t, err.Error(), "k6/api-baseline/http")
}

// The same measurement may be limited from above and from below at once: a
// latency ceiling and a throughput floor answer different questions.
func TestALimitAboveAndALimitBelowAreNotADuplicate(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(
		gate("quic/quic-agents/aggregate", "rps", "min", 10, true)+
			gate("quic/quic-agents/aggregate", "error_rate", "max", 0, true), "")))

	require.NoError(t, err)
}

// A measurement deliberately left unlimited says so, and says why.
//
// The set of limits this replaces had a catch-all: anything nobody listed was
// held to 1000 ms automatically. A profile has no catch-all, so moving the
// numbers across can delete protection while looking like tidying up. An entry
// here is how a measurement stays accounted for without being limited.
func TestAMeasurementLeftUnlimitedIsDeclaredWithItsReason(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "rps", "the rate is the pacing this scenario asks for"))))
	require.NoError(t, err)

	require.Len(t, p.Ungated, 1)
	assert.Equal(t, "k6/api-baseline/http", p.Ungated[0].Series)
	assert.Equal(t, "rps", p.Ungated[0].Metric)
	assert.NotEmpty(t, p.Ungated[0].Reason)
}

// An exemption whose reason is not written next to it cannot be reviewed and
// will never be removed.
func TestAnUnlimitedMeasurementWithNoReasonIsRefused(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		"  - series: k6/api-baseline/http\n    metric: rps\n")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reason")
}

// A measurement cannot be limited and declared unlimited at once. One of the
// two is wrong, and which one is not for a reader to guess.
func TestAMeasurementCannotBeBothLimitedAndDeclaredUnlimited(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "latency_p95_ms", "it is limited above, which is the contradiction"))))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "both limited and declared unlimited")
}

// The decisions a profile has recorded, in the shape a check reads them: what
// is limited, and what is deliberately not.
func TestAProfileReportsWhichMeasurementsItHasDecided(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		exemption("k6/api-baseline/http", "rps", "the rate is the pacing this scenario asks for"))))
	require.NoError(t, err)

	decided := p.DecidedMeasurements()
	assert.True(t, decided["k6/api-baseline/http|latency_p95_ms"], "a limited measurement is decided")
	assert.True(t, decided["k6/api-baseline/http|rps"], "a deliberately unlimited one is decided too")
	assert.False(t, decided["k6/api-baseline/http|error_rate"], "one nobody has ruled on is not")
}
