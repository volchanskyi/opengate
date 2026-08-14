package rules

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Which machines a staged rule has reached.
//
// The ids here are derived from a counter rather than drawn at random, so a case
// that fails fails the same way on the next run and on someone else's machine.
func fleet(size int) []uuid.UUID {
	out := make([]uuid.UUID, size)
	for i := range out {
		out[i] = uuid.NewSHA1(uuid.Nil, fmt.Appendf(nil, "device-%d", i))
	}
	return out
}

// reached counts how many of a fleet a rule at percent actually lands on.
func reached(devices []uuid.UUID, ruleID string, percent int) int {
	count := 0
	for _, id := range devices {
		if InStage(id, ruleID, percent, len(devices)) {
			count++
		}
	}
	return count
}

// Raising the reach only ever adds machines. A device that dropped out as the
// percentage rose would have the rule installed, removed and installed again
// across one rollout — flapping a customer's estate for no reason and making
// every other property here untestable.
func TestStageMembershipOnlyEverAddsMachines(t *testing.T) {
	t.Parallel()

	devices := fleet(500)
	const ruleID = "disk-critical"

	in := make(map[uuid.UUID]int, len(devices))
	for percent := 1; percent <= 100; percent++ {
		for _, id := range devices {
			if !InStage(id, ruleID, percent, len(devices)) {
				assert.Zerof(t, in[id],
					"%s was in at %d%% and out again at %d%%", id, in[id], percent)
				continue
			}
			if in[id] == 0 {
				in[id] = percent
			}
		}
	}
	assert.Len(t, in, len(devices), "every machine is in by 100%")
}

// Membership is a function of the machine, the rule and the reach, and of
// nothing else — including how many times it is asked.
func TestStageMembershipIsStable(t *testing.T) {
	t.Parallel()

	devices := fleet(50)
	for _, id := range devices {
		first := InStage(id, "disk-critical", 30, len(devices))
		for range 5 {
			assert.Equal(t, first, InStage(id, "disk-critical", 30, len(devices)),
				"the same question must keep the same answer")
		}
	}
}

// One rule's canary must not be the same machines as another's, or the same
// handful of endpoints would carry every trial the fleet ever runs.
func TestStageMembershipDiffersByRule(t *testing.T) {
	t.Parallel()

	devices := fleet(1000)
	disk := make(map[uuid.UUID]bool)
	for _, id := range devices {
		if InStage(id, "disk-critical", 10, len(devices)) {
			disk[id] = true
		}
	}

	same, cpu := 0, 0
	for _, id := range devices {
		if !InStage(id, "cpu-saturated", 10, len(devices)) {
			continue
		}
		cpu++
		if disk[id] {
			same++
		}
	}
	require.NotZero(t, cpu)
	assert.Less(t, same, cpu, "two rules at one reach must not pick one identical set")
}

// The population a stage aims at. The floor is what makes a canary meaningful on
// a small estate — 1 % of a dental practice's twelve machines is nobody — and the
// fleet is what bounds it, because a floor of five cannot be met by three.
func TestStagePopulationHasACanaryFloorBoundedByTheFleet(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		percent   int
		fleetSize int
		want      int
		because   string
	}{
		{"canary of a mid-size estate", 1, 200, 5, "1 % of 200 is 2, and the floor is 5"},
		{"canary of a large estate", 1, 2000, 20, "1 % of 2000 is past the floor"},
		{"canary of a three-machine estate", 1, 3, 3, "the floor cannot exceed the fleet"},
		{"canary of a single machine", 1, 1, 1, ""},
		{"staged", 10, 2000, 200, ""},
		{"staged rounds up", 10, 55, 6, "a tenth of 55 is 5.5 machines, which is 6"},
		{"staged of a small estate", 10, 25, 5, "a tenth of 25 is under the floor the canary already met"},
		{"staged of a tiny estate", 10, 3, 3, "a stage never reaches fewer than the one before it"},
		{"full", 100, 2000, 2000, ""},
		{"not rolled out", 0, 2000, 0, ""},
		{"no fleet to reach", 1, 0, 0, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, StagePopulation(tc.percent, tc.fleetSize), tc.because)
		})
	}
}

// A stage never reaches fewer machines than the stage before it, whatever the
// fleet size does to the arithmetic — a rollout that shrank on its way forward
// would pull a rule back off machines it was already proving itself on.
func TestStagePopulationNeverShrinksAsARolloutAdvances(t *testing.T) {
	t.Parallel()

	for _, fleetSize := range []int{1, 3, 12, 25, 200, 2000, 5000} {
		canary := StagePopulation(PercentFor(StageCanary), fleetSize)
		staged := StagePopulation(PercentFor(StageStaged), fleetSize)
		full := StagePopulation(PercentFor(StageFull), fleetSize)

		assert.LessOrEqualf(t, canary, staged, "fleet of %d: staged is at least the canary", fleetSize)
		assert.LessOrEqualf(t, staged, full, "fleet of %d: full is at least staged", fleetSize)
		assert.Equalf(t, fleetSize, full, "fleet of %d: full is the whole estate", fleetSize)
	}
}

// The population is what a stage aims at; membership is what it hits, one
// machine at a time and without the fleet's roster. The two must land in the
// same place, or a canary aiming at five machines would quietly cover half an
// estate.
func TestStageMembershipLandsNearThePopulationItAimsAt(t *testing.T) {
	t.Parallel()

	devices := fleet(2000)
	for _, percent := range []int{1, 10, 50} {
		want := StagePopulation(percent, len(devices))
		got := reached(devices, "disk-critical", percent)
		assert.InDeltaf(t, want, got, float64(want)/2+3,
			"a %d%% rollout aims at %d machines and reached %d", percent, want, got)
	}
}

// The two ends are absolute: nothing is reached before a rollout starts, and
// nothing is left out once it finishes.
func TestStageMembershipCoversTheEndsExactly(t *testing.T) {
	t.Parallel()

	devices := fleet(300)
	assert.Zero(t, reached(devices, "disk-critical", 0), "a rule at 0 % reaches nobody")
	assert.Equal(t, len(devices), reached(devices, "disk-critical", 100),
		"a rule at 100 % reaches the whole estate")
}

// Without a fleet count the floor cannot be worked out, so the rule reaches the
// share it declares and never more. Guessing the other way would put a canary on
// an estate the moment a count query failed.
func TestStageMembershipWithoutAFleetCountReachesOnlyItsDeclaredShare(t *testing.T) {
	t.Parallel()

	devices := fleet(2000)
	count := 0
	for _, id := range devices {
		if InStage(id, "disk-critical", 1, 0) {
			count++
		}
	}
	assert.Less(t, count, len(devices)/10,
		"an unsized canary must stay near its 1 %, not spread to the estate")
	assert.Positive(t, count, "and must still reach the machines it names")
}

// What the pushed ruleset actually asks: does this machine get this rule. A stop
// outranks membership — a killed rule reaches nobody, including the canary that
// was proving it.
func TestRolloutReachesRespectsBothTheStopAndTheStage(t *testing.T) {
	t.Parallel()

	devices := fleet(200)
	org := uuid.New()

	full := DefaultRollout(org, "disk-critical")
	for _, id := range devices {
		assert.True(t, full.Reaches(id, len(devices)), "a rule nobody staged reaches every machine")
	}

	canary := full
	canary.RolloutPercent = PercentFor(StageCanary)
	in := 0
	for _, id := range devices {
		if canary.Reaches(id, len(devices)) {
			in++
		}
	}
	assert.Positive(t, in)
	assert.Less(t, in, len(devices), "a canary is not the estate")

	killed := canary
	killed.Kill = true
	for _, id := range devices {
		assert.False(t, killed.Reaches(id, len(devices)), "a kill stops the canary too")
	}
}

// A rollout state that needs a fleet count says so, so the count is read for the
// customers that are mid-rollout and for nobody else. Every customer paying for a
// query on every reconnect, to size a stage almost none of them are in, is the
// cost this avoids.
func TestNeedsFleetSizeOnlyForAPartialRollout(t *testing.T) {
	t.Parallel()

	org := uuid.New()
	full := DefaultRollout(org, "disk-critical")
	assert.False(t, NeedsFleetSize(map[string]Rollout{"disk-critical": full}),
		"a rule at full reach needs no count")

	off := full
	off.Enabled = false
	off.RolloutPercent = PercentFor(StageCanary)
	assert.False(t, NeedsFleetSize(map[string]Rollout{"disk-critical": off}),
		"a rule that reaches nobody needs no count either")

	canary := full
	canary.RolloutPercent = PercentFor(StageCanary)
	assert.True(t, NeedsFleetSize(map[string]Rollout{"disk-critical": full, "cpu-saturated": canary}),
		"one customer mid-rollout is what the count is for")
}

func TestStageForReadsTheStoredReach(t *testing.T) {
	t.Parallel()

	tests := []struct {
		percent int
		want    Stage
	}{
		{-1, StageOff},
		{0, StageOff},
		{1, StageCanary},
		{9, StageCanary},
		{10, StageStaged},
		{99, StageStaged},
		{100, StageFull},
		{101, StageFull},
	}

	for _, tc := range tests {
		assert.Equalf(t, tc.want, StageFor(tc.percent), "%d %% is the %s stage", tc.percent, tc.want)
	}
}
