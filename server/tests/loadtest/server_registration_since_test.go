package main

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationSinceAnEarlierReadingIsOnlyWhatCameAfter(t *testing.T) {
	before, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)
	after := before
	after.Accepted, after.Rejected, after.SumSeconds = 150, 4, 26.4
	after.Buckets = []bucketBound{{0.001, 0}, {0.005, 10}, {0.01, 60}, {0.025, 90}, {0.05, 98}, {0.5, 140}, {math.Inf(1), 150}}
	after.PoolInUse = 9

	since := after.Since(before)

	assert.Equal(t, int64(50), since.Accepted)
	assert.Equal(t, int64(1), since.Rejected)
	assert.InDelta(t, 500.0, since.MeanMs(), 0.5, "the walk's 50 took 25 s between them")
	assert.Greater(t, since.QuantileMs(0.5), 50.0, "the install's fast registrations are left out")
	assert.InDelta(t, 9.0, since.PoolInUse, 0, "the pool is the later reading's")
}

func TestRegistrationSinceNothingIsTheReadingItself(t *testing.T) {
	after, err := ParseServerRegistration(sampleMetricsPage)
	require.NoError(t, err)

	assert.Equal(t, after, after.Since(ServerRegistration{}))
}
