package main

import (
	"fmt"
	"os"
	"runtime"
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

// statLine is the first line of /proc/stat with the given user, system, idle, iowait and steal
// ticks, the other fields zero.
func statLine(user, system, idle, iowait, steal uint64) string {
	return fmt.Sprintf("cpu  %d 0 %d %d %d 0 0 %d 0 0\ncpu0 1 2 3 4 5 6 7 8 9 10\n",
		user, system, idle, iowait, steal)
}

func requireBusyPercent(t *testing.T, before, after string, want float64) {
	t.Helper()
	first, ok := parseCPUTimes(before)
	require.True(t, ok, "%q", before)
	second, ok := parseCPUTimes(after)
	require.True(t, ok, "%q", after)
	percent, ok := busyPercent(first, second)
	require.True(t, ok)
	assert.InDelta(t, want, percent, 0.01)
}

func TestTheBusyShareIsTheTimeSpentWorkingBetweenTwoReadings(t *testing.T) {
	// 200 busy ticks against 700 idle ones between the readings.
	requireBusyPercent(t, statLine(100, 100, 800, 0, 0), statLine(200, 200, 1500, 0, 0), 22.22)
}

func TestWaitingOnTheDiskIsRoomForTheRun(t *testing.T) {
	requireBusyPercent(t, statLine(0, 0, 0, 0, 0), statLine(10, 10, 60, 20, 0), 20)
}

func TestTimeTheHostTookBackIsNoRoomForTheRun(t *testing.T) {
	requireBusyPercent(t, statLine(0, 0, 0, 0, 0), statLine(10, 10, 40, 0, 40), 60)
}

func TestAQueueOfWaitingTasksOnAnIdleProcessorLeavesRoomToStart(t *testing.T) {
	// 2026-10-15 09:27: a load average of 2.97 on two processors while they were 22% busy.
	queued, ok := runQueuePercent("2.97 2.10 1.40 5/680 2442829\n", 2)
	require.True(t, ok)
	require.Error(t, CheckRoomToStart(tightSafety(), NodeReading{Measured: true, CPUPercent: queued}),
		"the queue alone reads as a node past its ceiling")

	first, _ := parseCPUTimes(statLine(1000, 1000, 7000, 70, 0))
	second, _ := parseCPUTimes(statLine(1220, 1220, 8700, 90, 0))
	busy, ok := busyPercent(first, second)
	require.True(t, ok)
	assert.NoError(t, CheckRoomToStart(tightSafety(), NodeReading{Measured: true, CPUPercent: busy}),
		"the processor itself had room")
}

func TestAnUnreadableProcessorAccountMeasuresNothing(t *testing.T) {
	for _, raw := range []string{"", "intr 1 2 3\n", "cpu  1 2 three 4\n", "cpu  1 2\n"} {
		_, ok := parseCPUTimes(raw)
		assert.False(t, ok, "%q is not a processor account", raw)
	}
	same, _ := parseCPUTimes(statLine(5, 5, 5, 5, 5))
	_, ok := busyPercent(same, same)
	assert.False(t, ok, "no time passed, so nothing was measured")
	later, _ := parseCPUTimes(statLine(9, 9, 9, 9, 9))
	_, ok = busyPercent(later, same)
	assert.False(t, ok, "counters that ran backwards measure a restart, not a machine at rest")
}

// statSequence serves /proc/stat readings in order, failing past the last.
func statSequence(raws ...string) func() (string, error) {
	next := 0
	return func() (string, error) {
		if next >= len(raws) {
			return "", os.ErrNotExist
		}
		next++
		return raws[next-1], nil
	}
}

func TestTheGuestReadingFirstSpansTenSecondsThenTheGapSinceItsLast(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	began := clock.now
	read := busyShareReading(clock, statSequence(
		statLine(0, 0, 0, 0, 0), statLine(30, 30, 40, 0, 0), statLine(40, 40, 160, 0, 0)))

	busy, ok := read()
	require.True(t, ok)
	assert.InDelta(t, 60, busy, 0.01)
	assert.Equal(t, 10*time.Second, clock.now.Sub(began), "the first reading covers ten seconds")

	busy, ok = read()
	require.True(t, ok)
	assert.InDelta(t, 14.29, busy, 0.01, "the second covers the time since the first")
	assert.Equal(t, 10*time.Second, clock.now.Sub(began), "and waits for nothing")
}

func TestTheVenuePicksWhichMeasureIsHonest(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	guest, ok := venueProcessorReading(true, clock, statSequence(
		statLine(0, 0, 0, 0, 0), statLine(10, 10, 80, 0, 0)), func() (string, error) {
		return "0.65 0.70 0.71 5/680 2442829\n", nil
	})()
	require.True(t, ok)
	assert.InDelta(t, 20, guest, 0.01, "a guest is read by the time its processors spent working")

	owner, ok := venueProcessorReading(false, clock, statSequence(), func() (string, error) {
		return "0.65 0.70 0.71 9/680 2442829\n", nil
	})()
	require.True(t, ok)
	assert.InDelta(t, 8/float64(runtime.NumCPU())*100, owner, 0.001,
		"a run that owns the box is read by its queue at the instant")
}

func TestTheVenueReadingIsAnActualMeasurement(t *testing.T) {
	reading := NewVenueReader(NewRealClock())()
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
