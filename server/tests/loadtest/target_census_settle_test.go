package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// The run's count of the fleet leads the target's, and the lead is the target's
// own admission work.
//
// A machine has dialled, handshaken and asked to register before the target has
// put it in the map it publishes a count of, and between those two moments the
// target reads the machine's customer and its name out of the database. So the
// two counts differ by the arrival rate times those reads: nothing at all on a
// phase offering no arrivals, and tens of machines on a climbing one against a
// target with no processor left.
//
// The answer is not a wider allowance — a share of the fleet cannot express a
// quantity that has nothing to do with fleet size. It is to ask the question of
// a settled population: the run holds still while the target admits the
// machines it has already accepted, and says how long that took.

// holding is a fleet of a fixed size, for a case that is not about machines
// leaving. The real floor falls as they do, which
// sequence_census_window_test.go is the subject of.
func holding(machines int) func() int { return func() int { return machines } }

// asking takes one census for a fleet of machines against a target whose count
// is the next answer in the list, and which keeps answering with the last.
func asking(machines int, answers ...int) CensusReading {
	i := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		held := float64(answers[min(i, len(answers)-1)])
		i++
		return TargetHealth{Read: true, Goroutines: held*3 + 29, AgentsConnected: &held}, true
	}}
	return census.Take(holding(machines), &testClock{now: time.Unix(1_800_000_000, 0)})
}

// What a census settles on, and what it held still for.
func TestWhatACensusSettlesOn(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		fleet   int
		answers []int
		settled int
		waited  time.Duration
	}{{
		// The ordinary case, and the one every healthy leg takes.
		name:  "a target that already accounts for the fleet is asked once",
		fleet: 500, answers: []int{500}, settled: 500, waited: 0,
	}, {
		// A machine that arrived while the question was in flight, which is the
		// run's own count catching up rather than a fleet that was never there.
		name:  "a target holding more than the run counted is not waited on",
		fleet: 500, answers: []int{502}, settled: 502, waited: 0,
	}, {
		// The drain and the recovery, where the rule over these counts matters
		// most, pay nothing for it.
		name:  "a phase with no fleet to account for has nothing to wait for",
		fleet: 0, answers: []int{0}, settled: 0, waited: 0,
	}, {
		// The repair, on the night's own numbers: a phase holding 7,946
		// machines against a target holding 7,883, with nothing failing and no
		// machine severed. Every one of those 63 was a connection the target
		// had accepted — the goroutine count taken in the same read was 24,821,
		// where 7,883 admitted machines account for about 23,900.
		name:  "a target still admitting what it accepted is waited out",
		fleet: 7946, answers: []int{7883, 7920, 7946}, settled: 7946, waited: 2 * censusSettleInterval,
	}, {
		// The finding survives. A target that was not holding the fleet does
		// not settle, so the run stops asking and publishes its answer.
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

// A target that stops answering part-way through is a question that never got
// an answer. The reading it gave before falling silent describes an instant the
// run can no longer bracket, so the census says the target went quiet rather
// than reporting a fleet it half-heard.
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

// The wait is bounded by the clock rather than by the asking, because reading
// the target's page is itself something a loaded target is slow at: the read is
// given fifteen seconds of its own, so a budget spent in questions asked would
// hold the end of a phase open for a quarter of an hour on exactly the target
// already struggling to answer.
func TestTheWaitIsBoundedByTheClockAndNotByTheAsking(t *testing.T) {
	t.Parallel()

	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	asked := 0
	census := TargetCensus{Read: func() (TargetHealth, bool) {
		asked++
		// A page this target takes its whole allowance to answer with.
		clock.Sleep(serverMetricsTimeout)
		held := 0.0
		return TargetHealth{Read: true, Goroutines: 29, AgentsConnected: &held}, true
	}}

	reading := census.Take(holding(500), clock)

	assert.LessOrEqual(t, reading.Waited, censusSettleLimit+serverMetricsTimeout,
		"a run does not hold a phase open past what it said it would for")
	assert.Less(t, asked, 5, "and the time the reads themselves cost is time spent waiting")
}

// The limit clears the widest arrival the target can report. Registration is
// the database work one arrival costs, and the histogram the server publishes
// it in tops out at ten seconds — past which a registration reads as a floor
// rather than a measurement, and fails every limit written for it anyway.
func TestTheWaitClearsTheWidestArrivalTheTargetCanReport(t *testing.T) {
	t.Parallel()

	buckets := appmetrics.RegistrationDurationBuckets()
	widest := time.Duration(buckets[len(buckets)-1] * float64(time.Second))
	assert.GreaterOrEqual(t, censusSettleLimit, 3*widest,
		"the run holds still for longer than the slowest arrival the target has a bucket for")
	assert.Less(t, censusSettleInterval, widest,
		"and asks often enough to see a target catching up")
}
