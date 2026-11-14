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

func TestAProfileThatLeavesAMeasurementUnlimitedIsRefused(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(apiLatency(true, 200),
		"  - series: k6/api-baseline/http\n    metric: rps\n    reason: the rate is the pacing\n")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "every measurement carries a limit")
}

func followingDeferrals(follows string, extra string) string {
	return gate("catchup/gaps/drain", "fill_seconds", "max", 960, true) + strings.Join([]string{
		"  - series: catchup/batches/drain",
		"    metric: deferred",
		"    follows: " + follows,
		"    blocking: false",
		extra,
	}, "\n")
}

func TestALimitMayFollowTheOutcomeOfAnotherLimit(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(followingDeferrals("catchup/gaps/drain|fill_seconds", ""), "")))

	require.NoError(t, err)
	require.Len(t, p.Gates, 2)
	assert.Equal(t, "catchup/gaps/drain|fill_seconds", p.Gates[1].Follows)
}

func TestAFollowingLimitNamesALimitOfItsOwnProfile(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(followingDeferrals("catchup/gaps/drain|machines_caught_up", ""), "")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "follows catchup/gaps/drain|machines_caught_up, which no limit of this profile is")
}

func TestAFollowingLimitStatesNoFigureOfItsOwn(t *testing.T) {
	_, err := ParseProfile([]byte(profileWith(
		followingDeferrals("catchup/gaps/drain|fill_seconds", "    max: 10\n"), "")))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "follows another limit and states a figure of its own")
}

func TestALimitMayFailTheNightOnlyFromAProcessorShare(t *testing.T) {
	p, err := ParseProfile([]byte(profileWith(
		gate("quic/quic-agents/register", "latency_p95_ms", "max", 500, true)+"    blocking_from_cpus: 1\n", "")))

	require.NoError(t, err)
	require.NotNil(t, p.Gates[0].BlockingFromCPUs)
	assert.InDelta(t, 1.0, *p.Gates[0].BlockingFromCPUs, 0)
}

func TestAProcessorShareBelowWhichALimitOnlyWatchesIsPositiveAndOnAFailingLimit(t *testing.T) {
	for name, gates := range map[string]string{
		"not positive": gate("quic/quic-agents/register", "latency_p95_ms", "max", 500, true) + "    blocking_from_cpus: 0\n",
		"watched":      gate("quic/quic-agents/register", "latency_p95_ms", "max", 500, false) + "    blocking_from_cpus: 1\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseProfile([]byte(profileWith(gates, "")))

			require.Error(t, err)
			assert.Contains(t, err.Error(), "blocking_from_cpus")
		})
	}
}

func walkWith(gates string) string {
	return `
schema_version: 1
name: shapes
family: spike
environment: runner
fixture: small
phases:
  - name: ramp
    duration: 1m
    connected_agents: 500
  - name: peak
    duration: 2m
    connected_agents: 2000
  - name: drain
    duration: 1m
    connected_agents: 0
  - name: recovery
    duration: 2m
    connected_agents: 500
  - name: hold
    duration: 2m
    connected_agents: 500
safety:
  max_node_memory_percent: 90
  max_error_rate: 0.01
gates:
` + gates
}

func TestASignInLimitSitsOnAPhaseMachinesJoin(t *testing.T) {
	for _, phase := range []string{"ramp", "peak", "recovery"} {
		t.Run(phase, func(t *testing.T) {
			_, err := ParseProfile([]byte(walkWith(gate("quic/register/"+phase, "latency_p95_ms", "max", 500, true))))

			require.NoError(t, err)
		})
	}
}

func TestASignInLimitOnAPhaseNoMachineJoinsIsRefused(t *testing.T) {
	for _, phase := range []string{"drain", "hold"} {
		t.Run(phase, func(t *testing.T) {
			_, err := ParseProfile([]byte(walkWith(
				gate("quic/register/"+phase, "latency_p50_ms", "max", 100, false) +
					gate("quic/register/"+phase, "latency_p95_ms", "max", 500, true))))

			require.Error(t, err)
			assert.Contains(t, err.Error(), `quic/register/`+phase)
			assert.Contains(t, err.Error(), `phase "`+phase+`" holds no more machines than the phase before it`)
			assert.Equal(t, 1, strings.Count(err.Error(), `phase "`+phase+`"`), "one refusal per phase")
		})
	}
}

func TestASignInLimitOnAFirstPhaseOfNoMachinesIsRefused(t *testing.T) {
	document := strings.Replace(walkWith(gate("quic/register/ramp", "latency_p95_ms", "max", 500, true)),
		"connected_agents: 500\n  - name: peak", "connected_agents: 0\n  - name: peak", 1)

	_, err := ParseProfile([]byte(document))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `phase "ramp" holds no more machines than the phase before it`)
}

func TestASignInLimitOnAPhaseTheProfileDoesNotWalkIsRefused(t *testing.T) {
	_, err := ParseProfile([]byte(walkWith(gate("quic/register/plateau", "latency_p95_ms", "max", 500, true))))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `quic/register/plateau names phase "plateau", which this profile does not walk`)
}
