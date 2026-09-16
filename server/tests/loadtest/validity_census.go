package main

import (
	"fmt"
)

// A level a phase publishes is not a level anything held.
//
// `achieved_connected_agents` is the run's own count of the machines that
// arrived and have not ended. That is one end of a conversation: it says the
// run's bookkeeping ran. The conservation bracket reads the target at the run's
// start and at its end, so every phase between them went unwatched — and two
// families published a recovery figure describing a target still carrying the
// full fleet on every night they ran, with no gate anywhere disagreeing.
//
// These are the rules that disagree, over the target's own account of the same
// population, taken where the phase takes its own.

// The two counts are of one population and describe one instant. The run counts
// its own arrived machines, asks the target, and counts again, so the target's
// answer is bracketed by the run's; and the target works its count out where the
// page is read rather than copying it in on a timer, so its answer is the
// population at the moment of the question.
//
// Both halves of that had to be true before this rule could say anything. A run
// counting machines it had only queued to dial claimed two thousand on a
// quarter-processor target where registering took eight seconds and a hundred
// and thirty-seven of them had not arrived. A target copying its count in every
// five seconds answered with the fleet of five seconds ago, which on a climb is
// short by the arrival rate times the interval: eight machines at 1.7 arrivals a
// second, sixty-four at 13.3, two hundred at 35.2, and a phase holding five
// hundred refused for a server "holding" four hundred and fifty-eight. The
// goroutine count in the same read — which Go works out when asked — agreed with
// the run throughout.
//
// So there is no tolerance here, and deliberately none: a share of the fleet
// cannot express a quantity that has nothing to do with fleet size, and a share
// wide enough to swallow one is wide enough to swallow the finding. What is
// allowed is what the fleet itself recorded leaving between the two readings,
// which is a count rather than an estimate.

// minTargetGoroutinesPerAgent is what the target's goroutine count has to clear
// for the fleet the phase claims.
//
// One apiece, which the listener guarantees: `go s.accept(ctx, conn)` runs a
// goroutine per accepted connection. Measured it is 3.00 per machine at both
// sizes — 630 goroutines at 200 machines and 1,530 at 500, against 29 at rest —
// so the floor is conservative by three times over, deliberately: it is here to
// refuse a count with no population behind it, not to state what a healthy
// server costs per machine.
const minTargetGoroutinesPerAgent = 1.0

// censusFleetFloor is the fewest machines the run can have been holding at the
// instant the target answered.
//
// The population changes by machines arriving and machines leaving. Between the
// run's two counts it can only have dipped below both of them by machines that
// left, and the fleet counts those, so this is a bound rather than a guess.
func censusFleetFloor(phase PhaseResult) int {
	lowest := phase.AchievedConnectedAgents
	if phase.ConnectedAgentsBeforeCensus < lowest {
		lowest = phase.ConnectedAgentsBeforeCensus
	}
	return lowest - int(phase.DeparturesDuringCensus)
}

// censusReasons collects the ways a phase's level was a level nobody held.
//
// These two put the target's own account beside the run's — its count of the
// same population, and the goroutines that bound that count below — so a phase
// describing a load the target was not carrying says so.
//
// It invalidates rather than fails. A phase whose target did not hold the fleet
// it counted did not measure the system under that load, and the numbers beside
// it are readings of something else.
func censusReasons(phase PhaseResult) []string {
	// A reading nobody could take is not a reading of nought, and nought is
	// exactly what these two rules act on. The phase says why it is absent
	// where it can; either way there is nothing here to conclude from.
	if phase.TargetConnectedAgents == nil || phase.TargetGoroutines == nil {
		return nil
	}

	claimed := phase.AchievedConnectedAgents
	if claimed <= 0 {
		return nil
	}

	var reasons []string
	// Only a shortfall is a finding. A target holding more than the run counted
	// is a machine that arrived while the question was in flight — the run's own
	// count catching up, not a fleet that was never there.
	if floor := censusFleetFloor(phase); *phase.TargetConnectedAgents < floor {
		reasons = append(reasons, fmt.Sprintf(
			"phase %q was holding %d machines and the target was holding %d, with %d having left while the question was asked, so the level it published is not a load the system carried",
			phase.Name, floor, *phase.TargetConnectedAgents, phase.DeparturesDuringCensus))
	}
	if *phase.TargetGoroutines < float64(claimed)*minTargetGoroutinesPerAgent {
		reasons = append(reasons, fmt.Sprintf(
			"phase %q counted %d machines and the target was running %.0f goroutines, below the one per accepted connection its listener starts, so its count has no population behind it",
			phase.Name, claimed, *phase.TargetGoroutines))
	}
	return reasons
}
