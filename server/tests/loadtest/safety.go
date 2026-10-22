package main

import (
	"errors"
	"fmt"
)

// NodeReading is the machine the run shares, at one instant.
type NodeReading struct {
	// Measured reports whether the figures were read from the node.
	Measured      bool
	CPUPercent    float64
	MemoryPercent float64
	// DiskPercent is how full the database's filesystem is; zero when unread.
	DiskPercent float64
}

// SafetyReader takes one reading.
type SafetyReader func() NodeReading

// CheckRoomToStart reports why a run may not begin, or nil if it may. It alone reads the
// processor ceiling, since a reading under offered load measures the run's own work.
func CheckRoomToStart(limits Safety, reading NodeReading) error {
	if !reading.Measured {
		return errors.New("safety: the node was not measured, and an unmeasured node is not a node inside its limits")
	}

	var problems []error
	if limits.MaxNodeCPUPercent > 0 && reading.CPUPercent > limits.MaxNodeCPUPercent {
		problems = append(problems, fmt.Errorf(
			"the node's processor is %.0f%% committed against a limit of %.0f%% — production shares it",
			reading.CPUPercent, limits.MaxNodeCPUPercent))
	}
	return errors.Join(append(problems, CheckRoomToContinue(limits, reading))...)
}

// CheckRoomToContinue reports why a run already offering load must stop, or nil while it may
// carry on. It covers the ceilings on room the node can run out of.
func CheckRoomToContinue(limits Safety, reading NodeReading) error {
	if !reading.Measured {
		return errors.New("safety: the node was not measured, and an unmeasured node is not a node inside its limits")
	}

	var problems []error
	if limits.MaxNodeMemoryPercent > 0 && reading.MemoryPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's memory is %.0f%% used against a limit of %.0f%% — past this the kubelet starts evicting",
			reading.MemoryPercent, limits.MaxNodeMemoryPercent))
	}
	// Disk shares the memory ceiling, as both mean the node has no room left for the run's output.
	if limits.MaxNodeMemoryPercent > 0 && reading.DiskPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's disk is %.0f%% full against the same %.0f%% ceiling as its memory — the database writes there",
			reading.DiskPercent, limits.MaxNodeMemoryPercent))
	}
	return errors.Join(problems...)
}

// walkStartedAnnouncement is printed when the walk starts, followed by the start second;
// scripts/loadtest-quic-incluster.sh reads it.
const walkStartedAnnouncement = "Walk started at"

// RunPhasesWatched walks a profile and stops the moment the machine it shares
// goes past what the profile said it would accept.
func RunPhasesWatched(profile *Profile, fleet Fleet, clock Clock, read SafetyReader, readings PhaseReadings) ([]PhaseResult, error) {
	if profile == nil {
		return nil, errors.New("run phases: no profile — a run without one has no phases to walk")
	}
	if len(profile.Phases) == 0 {
		return nil, errors.New("run phases: the profile declares no phases")
	}
	// The full check runs once before anything is offered; later checks cover room only.
	if err := CheckRoomToStart(profile.Safety, read()); err != nil {
		return nil, fmt.Errorf("stopping before phase %q: %w", profile.Phases[0].Name, err)
	}

	// The start time lets the browser-side generators, which begin once the estate is filed,
	// join the shape at the right phase.
	fmt.Printf("%s %d\n", walkStartedAnnouncement, clock.Now().Unix())

	results := make([]PhaseResult, 0, len(profile.Phases))
	from := 0
	for i, phase := range profile.Phases {
		if i > 0 {
			if err := CheckRoomToContinue(profile.Safety, read()); err != nil {
				return results, fmt.Errorf("stopping before phase %q: %w", phase.Name, err)
			}
		}
		result, err := runOnePhase(phase, from, fleet, clock, readings)
		if err != nil {
			return nil, fmt.Errorf("phase %q: %w", phase.Name, err)
		}
		if err := CheckRoomToContinue(profile.Safety, read()); err != nil {
			return append(results, result), fmt.Errorf("stopping after phase %q: %w", phase.Name, err)
		}
		results = append(results, result)
		from = result.AchievedConnectedAgents
	}
	return results, nil
}
