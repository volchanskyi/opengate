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

func TestARunUnderEveryLimitCarriesOn(t *testing.T) {
	breach := CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 40, MemoryPercent: 55,
	})
	assert.NoError(t, breach)
}

func TestARunPastTheProcessorLimitStops(t *testing.T) {
	err := CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 92, MemoryPercent: 55,
	})
	require.Error(t, err)
	// The message names the machine the run shares, because that is what the
	// limit is about: production sits on the same node.
	assert.Contains(t, err.Error(), "processor")
}

func TestARunPastTheMemoryLimitStops(t *testing.T) {
	err := CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 10, MemoryPercent: 95,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory")
}

// A reading nobody took is not a reading of zero. Treating an absent measurement
// as "well within the limit" is how a guard comes to protect nothing.
func TestAnUnmeasuredNodeDoesNotPassTheLimit(t *testing.T) {
	err := CheckSafety(tightSafety(), NodeReading{Measured: false})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not measured")
}

func TestTheSequencerStopsWhenTheNodeIsPastItsLimit(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	readings := 0
	safe := func() NodeReading {
		readings++
		if readings > 3 {
			return NodeReading{Measured: true, CPUPercent: 99, MemoryPercent: 40}
		}
		return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 40}
	}

	_, err := RunPhasesWatched(profile, fleet, clock, safe)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "processor")
}

func TestTheSequencerRunsToTheEndWhileTheNodeHolds(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, func() NodeReading {
		return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 40}
	})
	require.NoError(t, err)
	assert.Len(t, results, 3)
}

// The reading the runner stack takes of itself has to be a reading, not a
// placeholder: a guard that always reports plenty of room is a guard nobody can
// fail.
func TestTheLocalReadingIsAnActualMeasurement(t *testing.T) {
	reading := LocalNodeReading()
	assert.True(t, reading.Measured, "the process can read its own machine")
	assert.GreaterOrEqual(t, reading.MemoryPercent, 0.0)
	assert.LessOrEqual(t, reading.MemoryPercent, 100.0)
}

// The processor ceiling is a statement about a neighbour. A disposable stack has
// none — the job creates it and throws it away — and driving the processor to
// saturation is what the scaling sweep is for, so such a profile declares no
// processor ceiling and a saturated reading is not a reason to stop.
func TestADisposableStackIsNotHeldToAProcessorCeiling(t *testing.T) {
	runnerSafety := Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01}
	assert.NoError(t, CheckSafety(runnerSafety, NodeReading{
		Measured: true, CPUPercent: 240, MemoryPercent: 55,
	}))
}

// The room it can still run out of holds either way: past the memory ceiling the
// numbers describe a machine with nowhere to put them.
func TestADisposableStackStillStopsWhenItRunsOutOfMemory(t *testing.T) {
	runnerSafety := Safety{MaxNodeMemoryPercent: 90, MaxErrorRate: 0.01}
	err := CheckSafety(runnerSafety, NodeReading{
		Measured: true, CPUPercent: 240, MemoryPercent: 95,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "memory")
}

// What the check claims to read is what the node is committed to now. The
// one-minute average is the minute before the check, and on a runner that minute
// is the one the job spent building images and a fleet — so reading it stopped
// every performance run before its first phase, naming the build as the run's
// own commitment.
func TestTheProcessorReadingIgnoresTheMinuteBeforeIt(t *testing.T) {
	// A machine whose last minute was fully committed and whose run queue is now
	// empty: nothing is running but this reader.
	assert.InDelta(t, 0.0, runQueuePercent("8.00 6.00 4.00 1/512 9931\n", 4), 0)
}

func TestTheProcessorReadingCountsWhatIsRunnableNow(t *testing.T) {
	// Nine runnable, one of them this reader, against four processors: the node
	// is committed to twice what it has.
	assert.InDelta(t, 200.0, runQueuePercent("0.00 0.00 0.00 9/512 9931\n", 4), 0)
}

// Reported as it is rather than trimmed to a hundred: a node committed to four
// times what it has and one exactly full are different findings, and a ceiling
// comparison works the same either way.
func TestTheProcessorReadingIsNotTrimmedToAHundred(t *testing.T) {
	assert.InDelta(t, 400.0, runQueuePercent("0.00 0.00 0.00 17/512 9931\n", 4), 0)
}

// A reading that could not be taken is not a reading of an idle machine, so the
// unreadable shapes return nothing rather than zero-as-a-measurement.
func TestAnUnreadableRunQueueMeasuresNothing(t *testing.T) {
	for _, raw := range []string{"", "0.00 0.00 0.00", "0.00 0.00 0.00 notanumber 9931"} {
		_, ok := parseRunQueue(raw)
		assert.False(t, ok, "%q is not a run-queue reading", raw)
	}
}

// Disk is held to the memory ceiling, and the message says so — a run stopped by
// a number nobody declared for disks is a run whose reason reads as a mistake.
func TestAFullDiskStopsTheRunAndNamesTheCeilingItUsed(t *testing.T) {
	err := CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: 10, MemoryPercent: 10, DiskPercent: 95,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk")
	assert.Contains(t, err.Error(), "same 90% ceiling as its memory")
}
