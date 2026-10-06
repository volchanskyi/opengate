package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// targetPage is an exposition carrying the four process families, the fleet count and extras.
func targetPage(goroutines, resident, fds, start string) string {
	return targetPageHolding(goroutines, resident, fds, start, "500")
}

// targetPageHolding is the same page with the fleet count set.
func targetPageHolding(goroutines, resident, fds, start, agents string) string {
	page := targetPageWithoutFleetCount(goroutines, resident, fds, start)
	if agents != "" {
		page += "opengate_agents_connected " + agents + "\n"
	}
	return page
}

// targetPageWithoutFleetCount is a page carrying every family except the fleet count.
func targetPageWithoutFleetCount(goroutines, resident, fds, start string) string {
	return "# HELP go_goroutines Number of goroutines that currently exist.\n" +
		"# TYPE go_goroutines gauge\n" +
		"go_goroutines " + goroutines + "\n" +
		"opengate_relay_active_sessions 0\n" +
		"process_resident_memory_bytes " + resident + "\n" +
		"process_open_fds " + fds + "\n" +
		"process_start_time_seconds " + start + "\n" +
		"process_max_fds 1.048576e+06\n"
}

func TestParseTargetHealthReadsTheFourProcessFamilies(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth(targetPage("7455", "3.6083e+08", "17", "1.7566e+09"))

	require.True(t, health.Read, "a page carrying the families is a reading")
	assert.Equal(t, 7455.0, health.Goroutines)
	assert.Equal(t, 3.6083e+08, health.ResidentBytes)
	assert.Equal(t, 17.0, health.OpenFDs)
	assert.Equal(t, 1.7566e+09, health.StartTimeSeconds)
}

func TestParseTargetHealthDoesNotReadAnAbsentPageAsZero(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth("<!doctype html><html><body>the single-page application</body></html>")

	assert.False(t, health.Read, "a page with no process families is not a reading")
}

func TestTargetConservationBracketing(t *testing.T) {
	t.Parallel()

	read := TargetHealth{Read: true, Goroutines: 30, StartTimeSeconds: 1000}

	assert.False(t, TargetConservation{End: read, Operations: 10}.Bracketed(),
		"a run with no start reading has no bracket")
	assert.False(t, TargetConservation{Start: read, Operations: 10}.Bracketed(),
		"a run with no end reading has no bracket")
	assert.False(t, TargetConservation{Start: read, End: read}.Bracketed(),
		"a bracket with nothing between its ends divides by nothing")
	assert.True(t, TargetConservation{Start: read, End: read, Operations: 10}.Bracketed())
}

func TestTargetConservationSeesTheProcessBeingReplaced(t *testing.T) {
	t.Parallel()

	same := TargetConservation{
		Start:      TargetHealth{Read: true, StartTimeSeconds: 1756000000},
		End:        TargetHealth{Read: true, StartTimeSeconds: 1756000000},
		Operations: 100,
	}
	assert.False(t, same.Restarted())

	replaced := same
	replaced.End.StartTimeSeconds = 1756000838
	assert.True(t, replaced.Restarted(), "a start time that moved is a different process")
}

func TestTargetConservationRetentionIsPerCompletedOperation(t *testing.T) {
	t.Parallel()

	leaking := TargetConservation{
		Start:      TargetHealth{Read: true, Goroutines: 29, ResidentBytes: 30 << 20, StartTimeSeconds: 1},
		End:        TargetHealth{Read: true, Goroutines: 2429, ResidentBytes: 130 << 20, StartTimeSeconds: 1},
		Operations: 1200,
	}
	assert.InDelta(t, 2.0, leaking.RetainedGoroutinesPerOperation(), 0.001)
	assert.InDelta(t, float64(100<<20)/1200, leaking.RetainedBytesPerOperation(), 1)

	settled := leaking
	settled.End.Goroutines = 27
	settled.End.ResidentBytes = 29 << 20
	assert.Equal(t, 0.0, settled.RetainedGoroutinesPerOperation())
	assert.Equal(t, 0.0, settled.RetainedBytesPerOperation())
}

func TestParseTargetHealthReadsTheServersOwnFleetCount(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth(targetPageHolding("1530", "3.6083e+08", "18", "1.7566e+09", "500"))

	require.NotNil(t, health.AgentsConnected, "a page carrying the count is a reading of it")
	assert.Equal(t, 500.0, *health.AgentsConnected)
}

func TestParseTargetHealthLeavesAnAbsentFleetCountAbsent(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth(targetPageWithoutFleetCount("1530", "3.6083e+08", "18", "1.7566e+09"))

	require.True(t, health.Read, "the page answered")
	assert.Nil(t, health.AgentsConnected, "a count nobody published is not a count of nought")
}
