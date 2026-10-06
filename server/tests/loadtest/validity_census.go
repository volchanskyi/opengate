package main

import (
	"fmt"
)

// minTargetGoroutinesPerAgent is the goroutines per claimed machine the target must show.
// The listener runs one goroutine per accepted connection, so one is the floor.
const minTargetGoroutinesPerAgent = 1.0

// censusFleetFloor is the fewest machines the run can have held when the target answered.
// Only departures counted by the fleet can lower the population below both of the run's counts.
func censusFleetFloor(phase PhaseResult) int {
	lowest := phase.AchievedConnectedAgents
	if phase.ConnectedAgentsBeforeCensus < lowest {
		lowest = phase.ConnectedAgentsBeforeCensus
	}
	return lowest - int(phase.DeparturesDuringCensus)
}

// censusReasons lists why a phase's published level describes a load the target did not carry.
func censusReasons(phase PhaseResult) []string {
	// An absent reading leaves nothing to conclude from.
	if phase.TargetConnectedAgents == nil || phase.TargetGoroutines == nil {
		return nil
	}

	claimed := phase.AchievedConnectedAgents
	if claimed <= 0 {
		return nil
	}

	var reasons []string
	// Only a shortfall counts; a target holding more is the run's own count catching up.
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
