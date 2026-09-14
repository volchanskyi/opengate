package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	agents, goroutines, absent := censusReading(
		targetPageHolding("1530", "3.6083e+08", "18", "1.7566e+09", "500")).Take()

	assert.Empty(t, absent, "a reading that was taken accounts for no absence")
	require.NotNil(t, agents)
	require.NotNil(t, goroutines)
	assert.Equal(t, 500, *agents)
	assert.Equal(t, 1530.0, *goroutines)
}

// A target loaded until it stops answering is the finding a capacity ladder
// climbs to reach, so the phase says so rather than reporting a fleet of nought.
func TestACensusOfASilentTargetIsAnAccountedAbsence(t *testing.T) {
	t.Parallel()

	agents, goroutines, absent := censusReading("<!doctype html><html><body>not an exposition</body></html>").Take()

	assert.Nil(t, agents)
	assert.Nil(t, goroutines)
	assert.Equal(t, censusAbsentTargetSilent, absent)
}

// The page answered and carries no count of the fleet. That is a different fact
// from a target that answered nothing, and a reader can tell them apart.
func TestACensusOfATargetThatPublishesNoFleetCountSaysSo(t *testing.T) {
	t.Parallel()

	agents, goroutines, absent := censusReading(
		targetPageWithoutFleetCount("1530", "3.6083e+08", "18", "1.7566e+09")).Take()

	assert.Nil(t, agents)
	assert.Nil(t, goroutines)
	assert.Equal(t, censusAbsentNoFleetCount, absent)
}

// A run pointed at no target asked nothing, so there is no absence to account
// for — the same rule the busy-ness reading beside it follows.
func TestACensusWithNoTargetToReadAccountsForNothing(t *testing.T) {
	t.Parallel()

	agents, goroutines, absent := TargetCensus{}.Take()

	assert.Nil(t, agents)
	assert.Nil(t, goroutines)
	assert.Empty(t, absent, "where there was no question there is no unanswered one")
}
