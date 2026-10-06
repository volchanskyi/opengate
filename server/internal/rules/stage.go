package rules

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// Stage is how far along a rollout is.
type Stage string

const (
	// StageOff is a rule that reaches nobody.
	StageOff Stage = "off"
	// StageCanary is the first handful of machines.
	StageCanary Stage = "canary"
	// StageStaged is a tenth of the estate.
	StageStaged Stage = "staged"
	// StageFull is the whole estate.
	StageFull Stage = "full"
)

const (
	// canaryFloorDevices is the fewest machines any partial stage reaches, capped by the fleet size.
	canaryFloorDevices = 5
	fullPercent        = 100
	// membershipBuckets is the resolution membership is decided at.
	membershipBuckets = 1_000_000
)

// StageFor reads the stage a stored reach puts a rule in at the shipped pace; see [Rollout.Stage].
func StageFor(percent int) Stage {
	return Rollout{RolloutPercent: percent}.Stage()
}

// PercentFor is the reach a stage rolls to at the shipped pace; see [Rollout.PercentForStage].
func PercentFor(stage Stage) int {
	return Rollout{}.PercentForStage(stage)
}

// StagePopulation is how many of a fleet of fleetSize a rollout at percent aims at.
// It never returns fewer than the stage before it would, so a rollout cannot shrink.
func StagePopulation(percent, fleetSize int) int {
	if fleetSize <= 0 || percent <= 0 {
		return 0
	}
	if percent >= fullPercent {
		return fleetSize
	}
	// Rounds up so a stage on a small estate reaches at least one machine.
	wanted := (fleetSize*percent + fullPercent - 1) / fullPercent
	return min(max(wanted, min(canaryFloorDevices, fleetSize)), fleetSize)
}

// InStage reports whether one machine is in the population a rule at percent has reached.
// A fleetSize of zero means an uncounted estate, so the rule reaches exactly its declared share.
func InStage(deviceID uuid.UUID, ruleID string, percent, fleetSize int) bool {
	limit := membershipLimit(percent, fleetSize)
	switch {
	case limit <= 0:
		return false
	case limit >= membershipBuckets:
		return true
	default:
		return membershipBucket(deviceID, ruleID) < limit
	}
}

// membershipLimit turns a stage's population into the share of the bucket space it occupies.
func membershipLimit(percent, fleetSize int) int64 {
	if fleetSize <= 0 {
		return int64(min(max(percent, 0), fullPercent)) * membershipBuckets / fullPercent
	}
	return int64(StagePopulation(percent, fleetSize)) * membershipBuckets / int64(fleetSize)
}

// membershipBucket places a machine in the bucket space, hashing the rule id in
// so each rule picks its own machines.
func membershipBucket(deviceID uuid.UUID, ruleID string) int64 {
	h := fnv.New64a()
	_, _ = h.Write(deviceID[:])
	_, _ = h.Write([]byte{'/'})
	_, _ = h.Write([]byte(ruleID))
	return int64(h.Sum64() % membershipBuckets)
}

// Reaches reports whether one machine gets this rule: it is delivered and the machine is in stage.
// A kill outranks membership, so a killed rule reaches no machine.
func (r Rollout) Reaches(deviceID uuid.UUID, fleetSize int) bool {
	return r.Delivers() && InStage(deviceID, r.RuleID, r.RolloutPercent, fleetSize)
}

// NeedsFleetSize reports whether any delivered rollout is mid-rollout and so needs the estate
// counted to size its stage.
func NeedsFleetSize(rollouts map[string]Rollout) bool {
	for _, r := range rollouts {
		if !r.Delivers() {
			continue
		}
		switch r.Stage() {
		case StageCanary, StageStaged:
			return true
		case StageOff, StageFull:
		}
	}
	return false
}
