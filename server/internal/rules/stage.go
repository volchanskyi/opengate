package rules

import (
	"hash/fnv"

	"github.com/google/uuid"
)

// How far a rule has reached, and which machines that is.
//
// A curated rule that turns out to be wrong is the one thing here that can
// degrade five thousand machines at once, so a rule reaches an estate in stages:
// a handful of machines, then a tenth of them, then all of them. What earns each
// step is in gate.go. What is here is the arithmetic of who is in.
//
// Membership is decided one machine at a time, from the machine, the rule and
// the reach — no roster, no stored list of chosen devices. That is what makes it
// the same answer on every server, before and after a restart, and what makes
// raising the reach add machines without ever taking one away. A rollout that
// dropped a machine as it grew would install a rule, remove it and install it
// again across one afternoon on somebody's file server.

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
	// canaryFloorDevices is the fewest machines a stage short of the whole estate
	// is worth running on. A percentage alone is meaningless on a small estate —
	// one percent of a dental practice's twelve machines is nobody, and a rollout
	// that reached nobody would advance on an hour of silence that proved
	// nothing. The floor is bounded by the fleet, because five cannot be met by
	// three, and it holds for every partial stage, because a rollout that shrank
	// on its way forward would pull a rule off machines already proving it.
	canaryFloorDevices = 5
	// fullPercent is the whole estate. The two partial stages' reaches are the
	// customer's to set and live on the rollout itself.
	fullPercent = 100
	// membershipBuckets is the resolution membership is decided at. Finer than
	// any estate this serves, so the share a stage aims at is not lost to
	// rounding on the way to a per-machine answer.
	membershipBuckets = 1_000_000
)

// StageFor reads the stage a stored reach puts a rule in, at the pace a customer
// who has configured nothing is on. A rollout that has been retuned reads its own
// populations — see [Rollout.Stage].
func StageFor(percent int) Stage {
	return Rollout{RolloutPercent: percent}.Stage()
}

// PercentFor is the reach a stage rolls to at the shipped pace. A retuned
// rollout reads its own — see [Rollout.PercentForStage].
func PercentFor(stage Stage) int {
	return Rollout{}.PercentForStage(stage)
}

// StagePopulation is how many of a fleet of fleetSize a rollout at percent aims
// at. It never returns fewer than the stage before it would, so a rollout moving
// forward cannot shrink.
func StagePopulation(percent, fleetSize int) int {
	if fleetSize <= 0 || percent <= 0 {
		return 0
	}
	if percent >= fullPercent {
		return fleetSize
	}
	// Round up: a tenth of fifty-five machines is six, not five. Rounding down is
	// how a stage on a small estate silently becomes no stage at all.
	wanted := (fleetSize*percent + fullPercent - 1) / fullPercent
	return min(max(wanted, min(canaryFloorDevices, fleetSize)), fleetSize)
}

// InStage reports whether one machine is in the population a rule at percent has
// reached, on an estate of fleetSize machines.
//
// A fleetSize of zero means the estate could not be counted. The rule then
// reaches the share it declares and no more: the canary floor cannot be worked
// out without a count, and guessing upward would spread a rule that is still
// being tried across an estate the moment a count query failed.
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

// membershipLimit turns a stage's population into the share of the bucket space
// it occupies. Monotone in percent, because StagePopulation is.
func membershipLimit(percent, fleetSize int) int64 {
	if fleetSize <= 0 {
		return int64(min(max(percent, 0), fullPercent)) * membershipBuckets / fullPercent
	}
	return int64(StagePopulation(percent, fleetSize)) * membershipBuckets / int64(fleetSize)
}

// membershipBucket places a machine in the bucket space, per rule. The rule id is
// part of it so two rules staged at once pick different machines — otherwise the
// same handful of endpoints would carry every trial the fleet ever runs.
func membershipBucket(deviceID uuid.UUID, ruleID string) int64 {
	h := fnv.New64a()
	// Hash writes never fail.
	_, _ = h.Write(deviceID[:])
	_, _ = h.Write([]byte{'/'})
	_, _ = h.Write([]byte(ruleID))
	return int64(h.Sum64() % membershipBuckets)
}

// Reaches reports whether one machine gets this rule: the customer has not
// stopped it, and the machine is in the population the rollout has reached so
// far. A stop outranks membership — a killed rule comes off the canary that was
// proving it, not just off the rest of the estate.
func (r Rollout) Reaches(deviceID uuid.UUID, fleetSize int) bool {
	return r.Delivers() && InStage(deviceID, r.RuleID, r.RolloutPercent, fleetSize)
}

// NeedsFleetSize reports whether any of a customer's stored rollout state is
// mid-rollout, and so needs the estate counted to size its stage. A customer
// whose rules are all at full reach — which is every customer who has staged
// nothing — needs no count, and paying for one on every machine's reconnect to
// size a stage nobody is in is the cost this avoids.
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
