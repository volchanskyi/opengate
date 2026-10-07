package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// holding is a fleet floor of a fixed size.
func holding(machines int) func() int { return func() int { return machines } }

// asking takes one census for a fleet of machines against a target that answers from the list.
func asking(machines int, answers ...int) CensusReading {
	i := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(answers[min(i, len(answers)-1)])
		i++
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}
	return census.Take(holding(machines), &testClock{now: time.Unix(1_800_000_000, 0)})
}

func TestWhatACensusSettlesOn(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		fleet   int
		answers []int
		settled int
		waited  time.Duration
	}{{
		name:  "a target that already accounts for the fleet is asked once",
		fleet: 500, answers: []int{500}, settled: 500, waited: 0,
	}, {
		name:  "a target holding more than the run counted is not waited on",
		fleet: 500, answers: []int{502}, settled: 502, waited: 0,
	}, {
		name:  "a phase with no fleet to account for has nothing to wait for",
		fleet: 0, answers: []int{0}, settled: 0, waited: 0,
	}, {
		name:  "a target still admitting what it accepted is waited out",
		fleet: 7946, answers: []int{7883, 7920, 7946}, settled: 7946, waited: 2 * censusSettleInterval,
	}, {
		name:  "a target that never catches up is what it last said",
		fleet: 500, answers: []int{0}, settled: 0, waited: censusSettleLimit,
	}} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			reading := asking(c.fleet, c.answers...)

			require.NotNil(t, reading.Agents)
			require.NotNil(t, reading.Goroutines)
			assert.Equal(t, c.settled, *reading.Agents, "the answer the target settled on")
			assert.Equal(t, float64(c.settled)*3+29, *reading.Goroutines, "both counts off the reading it stopped on")
			assert.Equal(t, c.waited, reading.Waited, "how long the run held still")
			assert.Empty(t, reading.Absent, "a target that answered accounts for no absence")
		})
	}
}

func TestACensusOfATargetThatFallsSilentMidWaitIsAnAccountedAbsence(t *testing.T) {
	t.Parallel()

	answered := false
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		if answered {
			return TargetHealth{}, false
		}
		answered = true
		held := 100.0
		return TargetHealth{Read: true, Goroutines: 329, AgentsConnected: &held}, true
	}}

	reading := census.Take(holding(500), &testClock{now: time.Unix(1_800_000_000, 0)})

	assert.Nil(t, reading.Agents)
	assert.Nil(t, reading.Goroutines)
	assert.Equal(t, censusAbsentTargetSilent, reading.Absent)
}

func TestTheWaitIsBoundedByTheClockAndNotByTheAsking(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	asked := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		asked++
		clock.Sleep(serverMetricsTimeout)
		held := 0.0
		return TargetHealth{Read: true, Goroutines: 29, AgentsConnected: &held}, true
	}}

	reading := census.Take(holding(500), clock)

	assert.LessOrEqual(t, reading.Waited, censusSettleLimit+serverMetricsTimeout,
		"a run does not hold a phase open past what it said it would for")
	assert.LessOrEqual(t, asked, int(censusSettleLimit/serverMetricsTimeout)+1,
		"and the time the reads themselves cost is time spent waiting")
}

func TestTheWaitClearsTheWidestArrivalTheTargetCanReport(t *testing.T) {
	t.Parallel()

	buckets := appmetrics.RegistrationDurationBuckets()
	widest := time.Duration(buckets[len(buckets)-1] * float64(time.Second))
	assert.GreaterOrEqual(t, censusSettleLimit, 3*widest,
		"the run holds still for longer than the slowest arrival the target has a bucket for")
	assert.Less(t, censusSettleInterval, widest,
		"and asks often enough to see a target catching up")
}

func TestAPhaseThatWaitedForNothingSaysNought(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: time.Unix(1_800_000_000, 0), tickOnNow: time.Millisecond}
	held := 500.0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		return TargetHealth{Read: true, Goroutines: 1529, AgentsConnected: &held}, true
	}}

	reading := census.Take(holding(500), clock)

	assert.Zero(t, reading.Waited, "a target that answered for the whole fleet was not waited on at all")
}
