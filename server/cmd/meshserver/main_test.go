package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProductionScheduleIsComplete(t *testing.T) {
	require.NoError(t, productionSchedule.Validate())
}

func TestSessionSweepIsFrequentRelativeToItsGrace(t *testing.T) {
	assert.Less(t, productionSchedule.SessionSweep, productionSchedule.SessionGrace,
		"a row is collectable well before the next pass that could collect it")
}

func TestInvestigationsRefreshIsSlowerThanTheScrape(t *testing.T) {
	assert.GreaterOrEqual(t, productionSchedule.Investigations, 30*time.Second,
		"a count over tables that only grow is not recomputed at scrape speed")
}
