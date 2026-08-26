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

// A profile that declares no limits is asking for none, which is the runner
// stack: it is thrown away at the end of the job and shares nothing.
func TestAProfileWithNoLimitsIsNotGuarded(t *testing.T) {
	assert.NoError(t, CheckSafety(Safety{}, NodeReading{Measured: false}))
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
