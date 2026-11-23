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
		"process_max_fds 1.048576e+06\n" +
		"# TYPE opengate_process_instance_info gauge\n" +
		`opengate_process_instance_info{instance_id="` + targetInstance + `"} 1` + "\n"
}

// targetInstance is the ID the fake target names its one start by.
const targetInstance = "W4ZQ7KXN2M5PCR3TBV6YHJ8DFG"

func TestParseTargetHealthReadsTheFourProcessFamilies(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth(targetPage("7455", "3.6083e+08", "17", "1.7566e+09"))

	require.True(t, health.Read, "a page carrying the families is a reading")
	assert.Equal(t, 7455.0, health.Goroutines)
	assert.Equal(t, 3.6083e+08, health.ResidentBytes)
	assert.Equal(t, 17.0, health.OpenFDs)
	assert.Equal(t, targetInstance, health.InstanceID)
}

func TestParseTargetHealthDoesNotReadAnAbsentPageAsZero(t *testing.T) {
	t.Parallel()

	health := ParseTargetHealth("<!doctype html><html><body>the single-page application</body></html>")

	assert.False(t, health.Read, "a page with no process families is not a reading")
}

func TestTargetConservationBracketing(t *testing.T) {
	t.Parallel()

	read := TargetHealth{Read: true, Goroutines: 30, InstanceID: targetInstance}

	assert.False(t, TargetConservation{End: read, Operations: 10}.Bracketed(),
		"a run with no start reading has no bracket")
	assert.False(t, TargetConservation{Start: read, Operations: 10}.Bracketed(),
		"a run with no end reading has no bracket")
	assert.False(t, TargetConservation{Start: read, End: read}.Bracketed(),
		"a bracket with nothing between its ends divides by nothing")
	assert.True(t, TargetConservation{Start: read, End: read, Operations: 10}.Bracketed())
}

func TestTargetConservationReadsAClockStepUnderOneProcessAsNoRestart(t *testing.T) {
	t.Parallel()

	start := ParseTargetHealth(targetPage("29", "3.0e+07", "17", "1.75600000000e+09"))
	stepped := ParseTargetHealth(targetPage("31", "3.1e+07", "17", "1.75599999941e+09"))

	same := TargetConservation{Start: start, End: stepped, Operations: 100}
	assert.False(t, same.Restarted(), "a start time moved by the host clock is the same process")
	assert.False(t, same.Unidentified())
}

func TestTargetConservationSeesTheProcessBeingReplaced(t *testing.T) {
	t.Parallel()

	replaced := TargetConservation{
		Start:      TargetHealth{Read: true, InstanceID: targetInstance},
		End:        TargetHealth{Read: true, InstanceID: "R8M2X5KQ7TB3WN6PZC4HJYDFVG"},
		Operations: 100,
	}
	assert.True(t, replaced.Restarted(), "a new instance ID is a different process")
}

func TestTargetConservationSaysWhenAReadingNamesNoInstance(t *testing.T) {
	t.Parallel()

	named := TargetHealth{Read: true, InstanceID: targetInstance}
	anonymous := TargetHealth{Read: true}

	assert.True(t, TargetConservation{Start: anonymous, End: named}.Unidentified())
	assert.True(t, TargetConservation{Start: named, End: anonymous}.Unidentified())
	assert.False(t, TargetConservation{Start: named, End: named}.Unidentified())
	assert.False(t, TargetConservation{Start: named}.Unidentified(),
		"an end never read is the stopped-answering reason, not this one")
	assert.False(t, TargetConservation{Start: anonymous, End: anonymous}.Restarted(),
		"two readings naming nobody are judged unidentified, never restarted")
}

func TestTargetConservationSeesTheProcessGoAwayBeforeTheEnd(t *testing.T) {
	t.Parallel()

	answered := TargetHealth{Read: true, InstanceID: targetInstance}
	assert.True(t, TargetConservation{Start: answered}.StoppedAnswering())
	assert.False(t, TargetConservation{Start: answered, End: answered}.StoppedAnswering())
	assert.False(t, TargetConservation{}.StoppedAnswering(), "a target never read says nothing")
}

func TestTargetConservationRetentionIsPerCompletedOperation(t *testing.T) {
	t.Parallel()

	leaking := TargetConservation{
		Start:      TargetHealth{Read: true, Goroutines: 29, ResidentBytes: 30 << 20, InstanceID: targetInstance},
		End:        TargetHealth{Read: true, Goroutines: 2429, ResidentBytes: 130 << 20, InstanceID: targetInstance},
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
