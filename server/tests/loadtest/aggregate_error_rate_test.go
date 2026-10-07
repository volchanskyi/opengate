package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func observedValue(bundle *Bundle, series string) (float64, bool) {
	for _, observation := range bundle.Observations {
		if observation.Series == series {
			return observation.Value, true
		}
	}
	return 0, false
}

func TestABundleStatesItsAggregateErrorRate(t *testing.T) {
	bundle := bundleFrom(t, harnessResults(), true)

	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published, "the aggregate is a series a limit is held against")
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
}

func TestTheAggregateCountsTheFleetLessWhatTheRunStoodDown(t *testing.T) {
	results := append(harnessResults(),
		agentResult{err: context.Canceled},
		agentResult{err: context.Canceled})

	bundle := bundleFrom(t, results, true)

	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
}

func TestAFleetEntirelyStoodDownReportsNoErrorRate(t *testing.T) {
	bundle := bundleFrom(t, []agentResult{{err: context.Canceled}, {err: context.Canceled}}, true)

	value, published := observedValue(bundle, "aggregate_error_rate")
	require.True(t, published)
	assert.Equal(t, 0.0, value)
}

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

func TestTheAggregateDividesByTheMachinesThatAskedNotTheFleetDeclared(t *testing.T) {
	in := measuredRun()
	in.Results = append(harnessResults(), harnessResults()...)
	in.AgentCount = 3

	value, published := observedValue(buildRunBundle(in), "aggregate_error_rate")
	require.True(t, published)
	assert.InDelta(t, 1.0/3.0, value, 0.0001)
	assert.GreaterOrEqual(t, value, 0.0, "a share of a fleet is never below nought")
}
