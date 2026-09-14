package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The share of the fleet that asked the server for something and did not get
// in. It is the one series the profiles hold their machine-side aggregate to,
// and a bundle that did not carry it left every one of those limits with
// nothing to read.

// observedValue is one series' value, and whether it was published at all.
func observedValue(bundle *Bundle, series string) (float64, bool) {
	for _, observation := range bundle.Observations {
		if observation.Series == series {
			return observation.Value, true
		}
	}
	return 0, false
}

// Two machines in, one refused: a third of the fleet did not get in.
func TestABundleStatesItsAggregateErrorRate(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published, "the aggregate is a series a limit is held against")
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
}

// A machine the run stood down never asked the server anything, so counting it
// as one the server refused publishes an error rate about the harness's own
// wind-down against a limit some profiles hold at nought. It is the denominator
// the canonical rows already divide by.
func TestTheAggregateCountsTheFleetLessWhatTheRunStoodDown(t *testing.T) {
	results := append(harnessResults(),
		agentResult{err: context.Canceled},
		agentResult{err: context.Canceled})

	bundle := bundleFrom(t, results, true)

	// Five offered, two stood down, three that asked: two arrived and one was
	// refused.
	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
}

// A run that stood its whole fleet down asked the system nothing. Dividing by
// the fleet would report every one of them as a machine the server refused.
func TestAFleetEntirelyStoodDownReportsNoErrorRate(t *testing.T) {
	bundle := bundleFrom(t, []agentResult{{err: context.Canceled}, {err: context.Canceled}}, true)

	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.Equal(t, 0.0, value)
}

// The three series the profiles hold their machine-side limits to, together: a
// bundle carrying two of them is a bundle two thirds of the limits can be read
// from.
func TestABundleCarriesEveryMachineSideSeriesALimitNames(t *testing.T) {
	in := measuredRun()
	reading, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)
	in.Registration = &reading

	series := observedSeries(buildRunBundle(in))
	for _, want := range []string{"aggregate_error_rate", "connect_p95_ms", "register_p95_ms"} {
		assert.True(t, series[want], "a limit names %s, so the bundle has to carry it", want)
	}
}

// An endurance run replaces a machine when it leaves, so it produces more
// machine-lives than the fleet it declared: the five-hour run of 2026-09-13
// declared 500 and reported 2,750 arrivals. Dividing by the declared fleet
// there makes the numerator larger than the denominator, and the share of a
// fleet that did not get in comes out below nought — which passes every ceiling
// a profile can write, because a ceiling is a maximum.
//
// So the denominator is the machines that asked, counted from the run's own
// results rather than from the number somebody declared. Every machine that
// arrived is one that asked, so the share cannot leave nought-to-one whatever
// the run did.
func TestTheAggregateDividesByTheMachinesThatAskedNotTheFleetDeclared(t *testing.T) {
	in := measuredRun()
	in.Results = append(harnessResults(), harnessResults()...)
	// What the run was told to offer, which churn has long since overtaken.
	in.AgentCount = 3

	value, published := observedValue(buildRunBundle(in), "aggregate_error_rate")
	require.True(t, published)
	// Six machine-lives, four arrivals, two refusals.
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
	assert.GreaterOrEqual(t, value, 0.0, "a share of a fleet is never below nought")
}
