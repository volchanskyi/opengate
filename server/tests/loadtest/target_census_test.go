package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askedOnce is the clock a single-reading census runs on.
func askedOnce() Clock { return &testClock{now: time.Unix(1_800_000_000, 0)} }

// censusReading is a census over the given exposition page.
func censusReading(page string) TargetCensus {
	return TargetCensus{Read: func() (TargetHealth, bool) {
		health := ParseTargetHealth(page)
		return health, health.Read
	}}
}

func TestACensusCarriesBothOfTheTargetsCounts(t *testing.T) {
	t.Parallel()

	reading := censusReading(
		targetPageHolding("1530", "3.6083e+08", "18", "1.7566e+09", "500")).Take(holding(500), askedOnce())

	assert.Empty(t, reading.Absent, "a reading that was taken accounts for no absence")
	require.NotNil(t, reading.Agents)
	require.NotNil(t, reading.Goroutines)
	assert.Equal(t, 500, *reading.Agents)
	assert.Equal(t, 1530.0, *reading.Goroutines)
}

func TestACensusOfASilentTargetIsAnAccountedAbsence(t *testing.T) {
	t.Parallel()

	reading := censusReading("<!doctype html><html><body>not an exposition</body></html>").Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Equal(t, censusAbsentTargetSilent, reading.Absent)
}

func TestACensusOfATargetThatPublishesNoFleetCountSaysSo(t *testing.T) {
	t.Parallel()

	reading := censusReading(
		targetPageWithoutFleetCount("1530", "3.6083e+08", "18", "1.7566e+09")).Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Equal(t, censusAbsentNoFleetCount, reading.Absent)
}

func TestACensusWithNoTargetToReadAccountsForNothing(t *testing.T) {
	t.Parallel()

	reading := TargetCensus{}.Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Empty(t, reading.Absent, "where there was no question there is no unanswered one")
}
