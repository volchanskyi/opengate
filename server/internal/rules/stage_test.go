package rules

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fleet(size int) []uuid.UUID {
	out := make([]uuid.UUID, size)
	for i := range out {
		out[i] = uuid.NewSHA1(uuid.Nil, fmt.Appendf(nil, "device-%d", i))
	}
	return out
}

func countWhere(devices []uuid.UUID, pick func(uuid.UUID) bool) int {
	count := 0
	for _, id := range devices {
		if pick(id) {
			count++
		}
	}
	return count
}

func reached(devices []uuid.UUID, ruleID string, percent int) int {
	return countWhere(devices, func(id uuid.UUID) bool {
		return InStage(id, ruleID, percent, len(devices))
	})
}

func rolloutReached(devices []uuid.UUID, r Rollout) int {
	return countWhere(devices, func(id uuid.UUID) bool { return r.Reaches(id, len(devices)) })
}

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

func TestStageMembershipDiffersByRule(t *testing.T) {
	t.Parallel()

	devices := fleet(1000)
	inRule := func(ruleID string) func(uuid.UUID) bool {
		return func(id uuid.UUID) bool { return InStage(id, ruleID, 10, len(devices)) }
	}
	inDisk, inCPU := inRule("disk-critical"), inRule("cpu-saturated")

	cpu := countWhere(devices, inCPU)
	same := countWhere(devices, func(id uuid.UUID) bool { return inCPU(id) && inDisk(id) })
	require.NotZero(t, cpu)
	assert.Less(t, same, cpu, "two rules at one reach must not pick one identical set")
}

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

func TestStageMembershipCoversTheEndsExactly(t *testing.T) {
	t.Parallel()

	devices := fleet(300)
	assert.Zero(t, reached(devices, "disk-critical", 0), "a rule at 0 % reaches nobody")
	assert.Equal(t, len(devices), reached(devices, "disk-critical", 100),
		"a rule at 100 % reaches the whole estate")
}

func TestStageMembershipWithoutAFleetCountReachesOnlyItsDeclaredShare(t *testing.T) {
	t.Parallel()

	devices := fleet(2000)
	count := countWhere(devices, func(id uuid.UUID) bool {
		return InStage(id, "disk-critical", 1, 0)
	})
	assert.Less(t, count, len(devices)/10,
		"an unsized canary must stay near its 1 %, not spread to the estate")
	assert.Positive(t, count, "and must still reach the machines it names")
}

func TestRolloutReachesRespectsBothTheStopAndTheStage(t *testing.T) {
	t.Parallel()

	devices := fleet(200)
	org := uuid.New()

	full := DefaultRollout(org, "disk-critical")
	assert.Equal(t, len(devices), rolloutReached(devices, full), "a rule nobody staged reaches every machine")

	canary := full
	canary.RolloutPercent = PercentFor(StageCanary)
	in := rolloutReached(devices, canary)
	assert.Positive(t, in)
	assert.Less(t, in, len(devices), "a canary is not the estate")

	killed := canary
	killed.Kill = true
	assert.Zero(t, rolloutReached(devices, killed), "a kill stops the canary too")
}

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
