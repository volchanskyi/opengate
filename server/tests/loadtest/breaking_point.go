package main

import "fmt"

// GaveOut is what counts as the system giving out: a rung crossing any declared term has given
// out, and a profile declaring the block states at least one.
type GaveOut struct {
	// ErrorRateAbove is the share of work the system refused or dropped.
	ErrorRateAbove float64 `yaml:"error_rate_above"`
	// LatencyP95MsAbove is the 95th-percentile wait in milliseconds.
	LatencyP95MsAbove float64 `yaml:"latency_p95_ms_above"`
	// TargetBusyPercentAbove is the share of its processor allowance the target used; it is
	// readable only where the run can reach the target's own exposition.
	TargetBusyPercentAbove float64 `yaml:"target_busy_percent_above"`
}

// declared reports whether any term was set.
func (g GaveOut) declared() bool {
	return g.ErrorRateAbove > 0 || g.LatencyP95MsAbove > 0 || g.TargetBusyPercentAbove > 0
}

// BreakingPoint is the ladder's answer.
type BreakingPoint struct {
	// HeldAt and HeldAgents are the last rung that stayed inside every term.
	// Empty when the very first rung gave out.
	HeldAt     string `json:"held_at,omitempty"`
	HeldAgents int    `json:"held_agents,omitempty"`
	// GaveAt and GaveAgents are the first rung that did not.
	// Empty when the ladder reached its top still holding.
	GaveAt     string `json:"gave_at,omitempty"`
	GaveAgents int    `json:"gave_agents,omitempty"`
	// Reason is the reading that decided it, in the words the profile's terms are written in.
	Reason string `json:"reason,omitempty"`
	// RungsRead is how many rungs the search looked at; a bundle whose answer read no rung is
	// refused, because "nothing gave out" would otherwise hold for a ladder that never arrived.
	RungsRead int `json:"rungs_read"`
	// Moved is every resource reading both rungs took where the two differ.
	Moved []MovedReading `json:"moved,omitempty"`
}

// MovedReading is one resource reading at the last rung that held and at the
// first that gave.
type MovedReading struct {
	Reading string  `json:"reading"`
	Held    float64 `json:"held"`
	Gave    float64 `json:"gave"`
}

// resourceReadings are the readings a phase takes of something that can run
// out, in the order they are reported, each with how to read it off a phase.
var resourceReadings = []struct {
	name string
	read func(PhaseResult) *float64
}{
	{"target_busy_percent", func(p PhaseResult) *float64 { return p.TargetBusyPercent }},
	{"generator_cpu_headroom_percent", func(p PhaseResult) *float64 { return p.GeneratorCPUHeadroomPercent }},
	{"generator_cpu_refused_percent", func(p PhaseResult) *float64 { return p.GeneratorCPURefusedPercent }},
	{"generator_udp_receive_errors", func(p PhaseResult) *float64 { return countAsFloat(p.GeneratorUDPReceiveErrors) }},
	{"target_udp_receive_errors", func(p PhaseResult) *float64 { return countAsFloat(p.TargetUDPReceiveErrors) }},
}

func countAsFloat(count *int64) *float64 {
	if count == nil {
		return nil
	}
	value := float64(*count)
	return &value
}

// movedBetween is every resource reading both rungs took that differs between
// them. A reading one of them could not take compares nothing.
func movedBetween(held, gave PhaseResult) []MovedReading {
	var moved []MovedReading
	for _, resource := range resourceReadings {
		before, after := resource.read(held), resource.read(gave)
		if before == nil || after == nil || *before == *after {
			continue
		}
		moved = append(moved, MovedReading{Reading: resource.name, Held: *before, Gave: *after})
	}
	return moved
}

// FindBreakingPoint reads a walk against the definition its profile declared, or returns nil.
// Only rungs are searched; a phase at or below the level before it is the wind-down.
func FindBreakingPoint(limits *GaveOut, phases []PhaseResult) *BreakingPoint {
	if limits == nil {
		return nil
	}

	answer := &BreakingPoint{}
	highest := 0
	var held *PhaseResult
	for i, phase := range phases {
		if phase.OfferedConnectedAgents <= highest {
			continue
		}
		highest = phase.OfferedConnectedAgents
		answer.RungsRead++

		if reason := gaveOutBecause(*limits, phase); reason != "" {
			answer.GaveAt = phase.Name
			answer.GaveAgents = phase.OfferedConnectedAgents
			answer.Reason = reason
			if held != nil {
				answer.Moved = movedBetween(*held, phase)
			}
			return answer
		}
		answer.HeldAt = phase.Name
		answer.HeldAgents = phase.OfferedConnectedAgents
		held = &phases[i]
	}
	return answer
}

// gaveOutBecause is why this rung gave out, or empty while it held.
// A term whose reading the run could not take decides nothing.
func gaveOutBecause(limits GaveOut, phase PhaseResult) string {
	if limits.ErrorRateAbove > 0 && phase.ErrorRate > limits.ErrorRateAbove {
		return fmt.Sprintf("error rate %.3f is past %.3f", phase.ErrorRate, limits.ErrorRateAbove)
	}
	if limits.LatencyP95MsAbove > 0 && phase.LatencyP95Ms > limits.LatencyP95MsAbove {
		return fmt.Sprintf("95th-percentile wait %.0f ms is past %.0f ms",
			phase.LatencyP95Ms, limits.LatencyP95MsAbove)
	}
	if limits.TargetBusyPercentAbove > 0 && phase.TargetBusyPercent != nil &&
		*phase.TargetBusyPercent > limits.TargetBusyPercentAbove {
		return fmt.Sprintf("the target used %.0f%% of its processor allowance, past %.0f%%",
			*phase.TargetBusyPercent, limits.TargetBusyPercentAbove)
	}
	return ""
}

func printBreakingPoint(answer *BreakingPoint) {
	if answer == nil {
		return
	}
	fmt.Printf("\n=== Where it gave ===\n")
	if answer.RungsRead == 0 {
		fmt.Printf("No rung was walked, so nothing was asked.\n")
		return
	}
	if answer.HeldAt != "" {
		fmt.Printf("Held:        %s (%d machines)\n", answer.HeldAt, answer.HeldAgents)
	}
	if answer.GaveAt == "" {
		fmt.Printf("Gave:        nothing did, over %d rungs — the answer is above this ladder\n", answer.RungsRead)
		return
	}
	fmt.Printf("Gave:        %s (%d machines)\n", answer.GaveAt, answer.GaveAgents)
	fmt.Printf("Because:     %s\n", answer.Reason)
	for _, moved := range answer.Moved {
		fmt.Printf("Moved:       %s %.1f → %.1f\n", moved.Reading, moved.Held, moved.Gave)
	}
}
