package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cadence the running server keeps. The product knows how to start its
// periodic workers; how often each one runs is this binary's choice, so the
// numbers are held to their relationships here rather than inside the package
// that only runs them.

// TestProductionScheduleIsComplete is what stands between a mistyped field and
// a worker that silently is not running: a zero duration panics the ticker on a
// goroutine nobody is watching.
func TestProductionScheduleIsComplete(t *testing.T) {
	require.NoError(t, productionSchedule.Validate())
}

// TestSessionSweepIsFrequentRelativeToItsGrace keeps an orphaned session row
// from lingering materially past the window it was supposed to survive.
func TestSessionSweepIsFrequentRelativeToItsGrace(t *testing.T) {
	assert.Less(t, productionSchedule.SessionSweep, productionSchedule.SessionGrace,
		"a row is collectable well before the next pass that could collect it")
}

// TestInvestigationsRefreshIsSlowerThanTheScrape states the property that keeps
// the gauges off the request path. The refresh is what reads the database; the
// scrape only reads what it left behind, so the two are deliberately not the
// same rate.
func TestInvestigationsRefreshIsSlowerThanTheScrape(t *testing.T) {
	assert.GreaterOrEqual(t, productionSchedule.Investigations, 30*time.Second,
		"a count over tables that only grow is not recomputed at scrape speed")
}
