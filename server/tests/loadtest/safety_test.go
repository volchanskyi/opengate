package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func tightSafety() Safety {
	return Safety{MaxNodeCPUPercent: 85, MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01}
}

func requireRefusalNaming(t *testing.T, err error, parts ...string) {
	t.Helper()
	require.Error(t, err)
	for _, part := range parts {
		assert.Contains(t, err.Error(), part)
	}
}

func runThreePhasesUnder(read SafetyReader) (*recordingFleet, []PhaseResult, error) {
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	results, err := RunPhasesWatched(threePhaseProfile(), fleet, clock, read, unreadTarget)
	return fleet, results, err
}

func steadyNode(cpu, memory float64) SafetyReader {
	return func() NodeReading {
		return NodeReading{Measured: true, CPUPercent: cpu, MemoryPercent: memory}
	}
}

func TestARunUnderEveryLimitCarriesOn(t *testing.T) {
	breach := CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 40, MemoryPercent: 55,
	})
	assert.NoError(t, breach)
}

func TestARunDoesNotStartOnANodeAlreadyPastTheProcessorLimit(t *testing.T) {
	err := CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 92, MemoryPercent: 55,
	})
	requireRefusalNaming(t, err, "processor")
}

func TestARunAlreadyOfferingLoadIsNotStoppedByABusyProcessor(t *testing.T) {
	assert.NoError(t, CheckRoomToContinue(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 108, MemoryPercent: 55,
	}))
}

func TestARunAlreadyOfferingLoadStopsWhenTheNodeRunsOutOfRoom(t *testing.T) {
	err := CheckRoomToContinue(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 20, MemoryPercent: 95,
	})
	requireRefusalNaming(t, err, "memory")

	err = CheckRoomToContinue(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 20, MemoryPercent: 10, DiskPercent: 95,
	})
	requireRefusalNaming(t, err, "disk")
}

func TestAnUnmeasuredNodeDoesNotPassEitherCheck(t *testing.T) {
	require.Error(t, CheckRoomToStart(tightSafety(), NodeReading{Measured: false}))
	require.Error(t, CheckRoomToContinue(tightSafety(), NodeReading{Measured: false}))
}

func TestARunPastTheMemoryLimitStops(t *testing.T) {
	err := CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 10, MemoryPercent: 95,
	})
	requireRefusalNaming(t, err, "memory")
}

func TestAnUnmeasuredNodeDoesNotPassTheLimit(t *testing.T) {
	err := CheckRoomToStart(tightSafety(), NodeReading{Measured: false})
	requireRefusalNaming(t, err, "not measured")
}

func TestTheSequencerStopsWhenTheNodeRunsOutOfRoom(t *testing.T) {
	readings := 0
	safe := func() NodeReading {
		readings++
		if readings > 3 {
			return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 99}
		}
		return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 40}
	}

	_, _, err := runThreePhasesUnder(safe)
	requireRefusalNaming(t, err, "memory")
}

func TestTheSequencerWillNotStartOnANodeProductionIsAlreadyFilling(t *testing.T) {
	fleet, _, err := runThreePhasesUnder(steadyNode(99, 40))
	requireRefusalNaming(t, err, "processor")
	assert.Contains(t, err.Error(), "before phase", "the answer is only about the neighbour before the run offers anything")
	assert.Empty(t, fleet.steps, "nothing was offered")
}

func TestTheSequencerCarriesOnThroughTheBusynessItIsCausing(t *testing.T) {
	readings := 0
	safe := func() NodeReading {
		readings++
		// Quiet before the walk, then fully committed by the run's own load.
		if readings > 1 {
			return NodeReading{Measured: true, CPUPercent: 190, MemoryPercent: 40}
		}
		return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 40}
	}

	_, results, err := runThreePhasesUnder(safe)
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestTheSequencerRunsToTheEndWhileTheNodeHolds(t *testing.T) {
	_, results, err := runThreePhasesUnder(steadyNode(20, 40))
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestTheLocalReadingIsAnActualMeasurement(t *testing.T) {
	reading := LocalNodeReading()
	assert.True(t, reading.Measured, "the process can read its own machine")
	assert.GreaterOrEqual(t, reading.MemoryPercent, 0.0)
	assert.LessOrEqual(t, reading.MemoryPercent, 100.0)
}

func TestADisposableStackIsNotHeldToAProcessorCeiling(t *testing.T) {
	runnerSafety := Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01}
	assert.NoError(t, CheckRoomToStart(runnerSafety, NodeReading{
		Measured: true, CPUPercent: 240, MemoryPercent: 55,
	}))
}

func TestADisposableStackStillStopsWhenItRunsOutOfMemory(t *testing.T) {
	runnerSafety := Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01}
	err := CheckRoomToStart(runnerSafety, NodeReading{
		Measured: true, CPUPercent: 240, MemoryPercent: 95,
	})
	requireRefusalNaming(t, err, "memory")
}

func requireRunQueuePercent(t *testing.T, raw string, want float64) {
	t.Helper()
	percent, ok := runQueuePercent(raw, 4)
	require.True(t, ok)
	assert.InDelta(t, want, percent, 0)
}

func TestTheProcessorReadingIgnoresTheMinuteBeforeIt(t *testing.T) {
	// The last minute was fully committed and the run queue is now empty but for this reader.
	requireRunQueuePercent(t, "8.00 6.00 4.00 1/512 9931\n", 0)
}

func TestTheProcessorReadingCountsWhatIsRunnableNow(t *testing.T) {
	// Nine runnable, one of them this reader, against four processors is twice committed.
	requireRunQueuePercent(t, "0.00 0.00 0.00 9/512 9931\n", 200)
}

func TestTheProcessorReadingIsNotTrimmedToAHundred(t *testing.T) {
	requireRunQueuePercent(t, "0.00 0.00 0.00 17/512 9931\n", 400)
}

func TestAnUnreadableRunQueueMeasuresNothing(t *testing.T) {
	for _, raw := range []string{"", "0.00 0.00 0.00", "0.00 0.00 0.00 notanumber 9931"} {
		_, ok := parseRunQueue(raw)
		assert.False(t, ok, "%q is not a run-queue reading", raw)

		_, ok = runQueuePercent(raw, 4)
		assert.False(t, ok, "%q reached the ceiling as a machine at rest", raw)
	}
	_, ok := runQueuePercent("0.00 0.00 0.00 9/512 9931\n", 0)
	assert.False(t, ok, "a machine with no processors is not a machine at rest")
}

func TestTheGuestReadingIsTheMinuteBeforeIt(t *testing.T) {
	percent, ok := loadAveragePercent("0.65 0.70 0.71 3/680 2442829\n", 2)
	require.True(t, ok)
	assert.InDelta(t, 32.5, percent, 0.01)
}

func TestAnUnreadableLoadAverageMeasuresNothing(t *testing.T) {
	for _, raw := range []string{"", "notanumber 0.70 0.71 3/680 1", "-1.00 0.70 0.71 3/680 1"} {
		_, ok := loadAveragePercent(raw, 2)
		assert.False(t, ok, "%q reached the ceiling as a machine at rest", raw)
	}
	_, ok := loadAveragePercent("0.65 0.70 0.71 3/680 1\n", 0)
	assert.False(t, ok, "a machine with no processors is not a machine at rest")
}

func TestTheTwoMeasuresDisagreeOnANodeThatIsNotBusy(t *testing.T) {
	const node = "0.65 0.70 0.71 5/680 2442829\n"
	const processors = 2

	instant, ok := runQueuePercent(node, processors)
	require.True(t, ok)
	require.Error(t, CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: instant,
	}), "the instant measure refuses this node")

	minute, ok := loadAveragePercent(node, processors)
	require.True(t, ok)
	require.NoError(t, CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: minute,
	}), "the minute measure lets it run")
}

func TestTheVenuePicksWhichMeasureIsHonest(t *testing.T) {
	const node = "0.65 0.70 0.71 5/680 2442829\n"

	guest, ok := venueProcessorMeasure(true)(node, 2)
	require.True(t, ok)
	assert.InDelta(t, 32.5, guest, 0.01, "a guest is read over the minute production shared with it")

	owner, ok := venueProcessorMeasure(false)(node, 2)
	require.True(t, ok)
	assert.InDelta(t, 200.0, owner, 0, "a run that owns the box is read at the instant")
}

func TestTheVenueReadingIsAnActualMeasurement(t *testing.T) {
	reading := VenueNodeReading()
	assert.True(t, reading.Measured, "the process can read the machine it is on")
	assert.GreaterOrEqual(t, reading.CPUPercent, 0.0)
	assert.GreaterOrEqual(t, reading.MemoryPercent, 0.0)
	assert.LessOrEqual(t, reading.MemoryPercent, 100.0)
}

func TestAFullDiskStopsTheRunAndNamesTheCeilingItUsed(t *testing.T) {
	err := CheckRoomToStart(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 10, MemoryPercent: 10, DiskPercent: 95,
	})
	requireRefusalNaming(t, err, "disk", "same 90% ceiling as its memory")
}
