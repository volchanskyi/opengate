package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// askedOnce is the clock a case that is about one reading hands the census.
// These cases are about what one answer carries; what a census does with a
// target that is behind is target_census_settle_test.go's subject.
func askedOnce() Clock { return &testClock{now: time.Unix(1_800_000_000, 0)} }

// What the target says it is holding, taken where a phase closes.
//
// A phase's own count of the fleet is bookkeeping the harness maintains: it
// answers whether the wind-down code ran. These two numbers are the target's
// answer to the same question, and a phase that carries both can be asked
// whether the load it describes was there.

// censusReading is a census over a page the case names, so each one reads as the
// one thing it is about.
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

// A target loaded until it stops answering is the finding a capacity ladder
// climbs to reach, so the phase says so rather than reporting a fleet of nought.
func TestACensusOfASilentTargetIsAnAccountedAbsence(t *testing.T) {
	t.Parallel()

	reading := censusReading("<!doctype html><html><body>not an exposition</body></html>").Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Equal(t, censusAbsentTargetSilent, reading.Absent)
}

// The page answered and carries no count of the fleet. That is a different fact
// from a target that answered nothing, and a reader can tell them apart.
func TestACensusOfATargetThatPublishesNoFleetCountSaysSo(t *testing.T) {
	t.Parallel()

	reading := censusReading(
		targetPageWithoutFleetCount("1530", "3.6083e+08", "18", "1.7566e+09")).Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Equal(t, censusAbsentNoFleetCount, reading.Absent)
}

// A run pointed at no target asked nothing, so there is no absence to account
// for — the same rule the busy-ness reading beside it follows.
func TestACensusWithNoTargetToReadAccountsForNothing(t *testing.T) {
	t.Parallel()

	reading := TargetCensus{}.Take(holding(500), askedOnce())

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Empty(t, reading.Absent, "where there was no question there is no unanswered one")
}
