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

	_, err := RunPhasesWatched(profile, fleet, clock, safe, unreadTarget)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "processor")
}

func TestTheSequencerRunsToTheEndWhileTheNodeHolds(t *testing.T) {
	profile := threePhaseProfile()
	fleet := &recordingFleet{}
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}

	results, err := RunPhasesWatched(profile, fleet, clock, func() NodeReading {
		return NodeReading{Measured: true, CPUPercent: 20, MemoryPercent: 40}
	}, unreadTarget)
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

// What the instant measure claims to read is what the box is committed to now.
// The one-minute average is the minute before the reading, and on a box the run
// owns that minute is the one the job spent building images and a fleet — so
// reading it stopped every performance run before its first phase, naming the
// build as the run's own commitment.
func TestTheProcessorReadingIgnoresTheMinuteBeforeIt(t *testing.T) {
	// A machine whose last minute was fully committed and whose run queue is now
	// empty: nothing is running but this reader.
	percent, ok := runQueuePercent("8.00 6.00 4.00 1/512 9931\n", 4)
	require.True(t, ok)
	assert.InDelta(t, 0.0, percent, 0)
}

func TestTheProcessorReadingCountsWhatIsRunnableNow(t *testing.T) {
	// Nine runnable, one of them this reader, against four processors: the node
	// is committed to twice what it has.
	percent, ok := runQueuePercent("0.00 0.00 0.00 9/512 9931\n", 4)
	require.True(t, ok)
	assert.InDelta(t, 200.0, percent, 0)
}

// Reported as it is rather than trimmed to a hundred: a node committed to four
// times what it has and one exactly full are different findings, and a ceiling
// comparison works the same either way.
func TestTheProcessorReadingIsNotTrimmedToAHundred(t *testing.T) {
	percent, ok := runQueuePercent("0.00 0.00 0.00 17/512 9931\n", 4)
	require.True(t, ok)
	assert.InDelta(t, 400.0, percent, 0)
}

// A reading that could not be taken is not a reading of an idle machine, so the
// unreadable shapes report nothing rather than zero-as-a-measurement — and they
// report it to the caller, not only to the parser. A measure that swallows its
// own "could not read" hands a ceiling the one answer that always passes.
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

// The other measure, and the one a guest reads: what the box was committed to
// over the last minute, against the processors it has.
func TestTheGuestReadingIsTheMinuteBeforeIt(t *testing.T) {
	// The live staging node, sampled from inside a pod: two processors, about a
	// third of one of them busy.
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

// The defect the venue split exists to close, demonstrated on one reading.
//
// A two-processor node a third busy, with five tasks runnable at the instant the
// reader looked. The instant measure divides four other runnable tasks across
// two processors and reports the node twice committed, which against an
// eighty-five percent ceiling refuses the run — and the same file says the node
// spent the last minute about a third busy. On a two-processor node the instant
// measure moves in fifty-point steps, so against that ceiling it is a coin flip
// on a node that is not busy.
func TestTheTwoMeasuresDisagreeOnANodeThatIsNotBusy(t *testing.T) {
	const node = "0.65 0.70 0.71 5/680 2442829\n"
	const processors = 2

	instant, ok := runQueuePercent(node, processors)
	require.True(t, ok)
	require.Error(t, CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: instant,
	}), "the instant measure refuses this node")

	minute, ok := loadAveragePercent(node, processors)
	require.True(t, ok)
	require.NoError(t, CheckSafety(tightSafety(), NodeReading{
		Measured: true, CPUPercent: minute,
	}), "the minute measure lets it run")
}

// Which measure is honest depends on whose box it is, so the venue picks and no
// call site has to remember which.
func TestTheVenuePicksWhichMeasureIsHonest(t *testing.T) {
	const node = "0.65 0.70 0.71 5/680 2442829\n"

	guest, ok := venueProcessorMeasure(true)(node, 2)
	require.True(t, ok)
	assert.InDelta(t, 32.5, guest, 0.01, "a guest is read over the minute production shared with it")

	owner, ok := venueProcessorMeasure(false)(node, 2)
	require.True(t, ok)
	assert.InDelta(t, 200.0, owner, 0, "a run that owns the box is read at the instant")
}

// Both readings this process can take of its own machine are readings, whichever
// venue it turns out to be on: a guard that always reports plenty of room is a
// guard nobody can fail.
func TestTheVenueReadingIsAnActualMeasurement(t *testing.T) {
	reading := VenueNodeReading()
	assert.True(t, reading.Measured, "the process can read the machine it is on")
	assert.GreaterOrEqual(t, reading.CPUPercent, 0.0)
	assert.GreaterOrEqual(t, reading.MemoryPercent, 0.0)
	assert.LessOrEqual(t, reading.MemoryPercent, 100.0)
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
