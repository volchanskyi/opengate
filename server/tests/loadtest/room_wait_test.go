package main

import (
	"encoding/json"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// timedReader serves the processor figures in order, repeating the last, and notes when each
// reading was taken on the clock.
type timedReader struct {
	clock   *testClock
	figures []float64
	memory  float64
	at      []time.Time
}

func (r *timedReader) read() NodeReading {
	r.at = append(r.at, r.clock.now)
	figure := r.figures[min(len(r.at), len(r.figures))-1]
	return NodeReading{Measured: true, CPUPercent: figure, MemoryPercent: r.memory}
}

func seededRoomWait(limit time.Duration) RoomWait {
	return RoomWait{Limit: limit, Rand: rand.New(rand.NewPCG(7, 11))}
}

// walkAnnouncedAt reads the second the walk announcement names out of what was printed.
func walkAnnouncedAt(t *testing.T, printed string) (int64, bool) {
	t.Helper()
	for _, line := range strings.Split(printed, "\n") {
		if second, found := strings.CutPrefix(line, walkStartedAnnouncement+" "); found {
			value, err := strconv.ParseInt(strings.TrimSpace(second), 10, 64)
			require.NoError(t, err)
			return value, true
		}
	}
	return 0, false
}

func TestAServerThatQuietensStartsTheRunAtTheFirstReadingWithRoom(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	reader := &timedReader{clock: clock, figures: []float64{92, 91, 20}, memory: 40}
	fleet := &recordingFleet{}

	ready, err := awaitRoom(tightSafety(), seededRoomWait(30*time.Minute), clock, reader.read)
	require.NoError(t, err)
	require.Len(t, reader.at, 3, "the third reading was the first with room")

	var results []PhaseResult
	printed := captureStdout(t, func() {
		results, err = RunPhasesWatched(threePhaseProfile(), fleet, clock, ready, unreadTarget)
	})
	require.NoError(t, err)
	assert.Len(t, results, 3)
	assert.Len(t, reader.at, 3+2*3-1, "the walk judged the reading that found room instead of taking another")
	announced, ok := walkAnnouncedAt(t, printed)
	require.True(t, ok, "the walk was announced: %q", printed)
	assert.Equal(t, reader.at[2].Unix(), announced, "the walk began at the reading that found room")
}

func TestAServerBusyForTheWholeWaitIsNeverWalkedAndSaysHowBusy(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	began := clock.now
	reader := &timedReader{clock: clock, figures: []float64{89, 93, 90}, memory: 40}

	_, err := awaitRoom(tightSafety(), seededRoomWait(30*time.Minute), clock, reader.read)

	var noRoom *NoRoomError
	require.ErrorAs(t, err, &noRoom)
	assert.Equal(t, 30*time.Minute, clock.now.Sub(began), "it waited the whole allowance and no longer")
	assert.Equal(t, "the shared server stayed 89–93% busy for 30 minutes, past the 85% the run allows",
		noRoom.Error())
	assert.Len(t, noRoom.Readings, len(reader.at), "every reading taken is reported")
}

func TestEveryWaitForRoomLiesInsideItsStepAndNoneIsLongerThanFiveMinutes(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	reader := &timedReader{clock: clock, figures: []float64{95}, memory: 40}

	_, err := waitForRoom(tightSafety(), seededRoomWait(30*time.Minute), clock, reader.read)
	require.Error(t, err)

	step := 15 * time.Second
	for i := 1; i < len(reader.at); i++ {
		gap := reader.at[i].Sub(reader.at[i-1])
		assert.LessOrEqual(t, gap, step, "wait %d", i)
		assert.LessOrEqual(t, gap, 5*time.Minute, "wait %d", i)
		if i < len(reader.at)-1 {
			assert.GreaterOrEqual(t, gap, step/2, "wait %d is at least half its step", i)
		}
		step = min(2*step, 5*time.Minute)
	}
	assert.Greater(t, len(reader.at), 6, "the waits reach the five-minute cap within the half hour")
}

func TestTheWaitsDifferFromOneSeedToAnother(t *testing.T) {
	gaps := func(seed uint64) []time.Duration {
		clock := &testClock{now: time.Unix(1_800_000_000, 0)}
		reader := &timedReader{clock: clock, figures: []float64{95}, memory: 40}
		wait := RoomWait{Limit: 10 * time.Minute, Rand: rand.New(rand.NewPCG(seed, seed))}
		_, _ = waitForRoom(tightSafety(), wait, clock, reader.read)
		out := make([]time.Duration, 0, len(reader.at))
		for i := 1; i < len(reader.at); i++ {
			out = append(out, reader.at[i].Sub(reader.at[i-1]))
		}
		return out
	}
	assert.Equal(t, gaps(3), gaps(3), "one seed spreads the waits the same way")
	assert.NotEqual(t, gaps(3), gaps(4), "the spread is random, so readings never lock onto a fixed clock")
}

func TestAWaitGivenNoSourceStillSpreadsItsPauses(t *testing.T) {
	wait := RoomWait{Limit: 30 * time.Minute}
	step := 4 * time.Minute
	seen := map[time.Duration]bool{}
	for range 50 {
		pause := wait.pause(step)
		assert.GreaterOrEqual(t, pause, step/2)
		assert.LessOrEqual(t, pause, step)
		seen[pause] = true
	}
	assert.Greater(t, len(seen), 1, "the pauses are spread, never the bare step")
}

func TestAServerOutOfMemoryIsRefusedAtOnceRatherThanWaitedFor(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	began := clock.now
	reader := &timedReader{clock: clock, figures: []float64{95}, memory: 97}

	_, err := waitForRoom(tightSafety(), seededRoomWait(30*time.Minute), clock, reader.read)
	requireRefusalNaming(t, err, "memory")
	var noRoom *NoRoomError
	assert.False(t, errors.As(err, &noRoom), "memory does not come back by waiting")
	assert.Equal(t, began, clock.now)
	assert.Len(t, reader.at, 1)
}

func TestABusyStartIsNeverAnnouncedAsAWalk(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet := &recordingFleet{}

	var err error
	printed := captureStdout(t, func() {
		_, err = RunPhasesWatched(threePhaseProfile(), fleet, clock, steadyNode(95, 40), unreadTarget)
	})
	requireRefusalNaming(t, err, "processor")
	_, announced := walkAnnouncedAt(t, printed)
	assert.False(t, announced, "the browser-side load joins only a walk that has begun")
	assert.Empty(t, fleet.steps)
}

func TestARunWithNoWaitRefusesAtTheFirstBusyReading(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	began := clock.now
	reader := &timedReader{clock: clock, figures: []float64{95}, memory: 40}

	_, err := waitForRoom(tightSafety(), RoomWait{}, clock, reader.read)
	requireRefusalNaming(t, err, "processor")
	assert.Equal(t, began, clock.now)
}

func TestARunThatFoundNoRoomLeavesItsReasonInTheBundle(t *testing.T) {
	dir := t.TempDir()
	refusal := &NoRoomError{Ceiling: 85, Waited: 30 * time.Minute, Readings: []float64{89, 93}}

	require.NoError(t, writeNotStartedBundle(dir, refusal))

	raw, err := os.ReadFile(filepath.Join(dir, "bundle.json"))
	require.NoError(t, err)
	var bundle struct {
		NotStarted struct {
			Reason         string    `json:"reason"`
			CeilingPercent float64   `json:"ceiling_percent"`
			WaitedSeconds  float64   `json:"waited_seconds"`
			Readings       []float64 `json:"readings_percent"`
		} `json:"not_started"`
	}
	require.NoError(t, json.Unmarshal(raw, &bundle))
	assert.Equal(t, refusal.Error(), bundle.NotStarted.Reason)
	assert.InDelta(t, 85, bundle.NotStarted.CeilingPercent, 0)
	assert.InDelta(t, 1800, bundle.NotStarted.WaitedSeconds, 0)
	assert.Equal(t, []float64{89, 93}, bundle.NotStarted.Readings)
}
