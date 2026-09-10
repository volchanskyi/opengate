package main

import "fmt"

// Where the system gives out, and what it means to give out.
//
// The breakpoint family raises the load until something gives and reports what
// gave. Until the profile writes down what "gives" means, the answer is whatever
// the run happened to survive: the 2026-09-09 ladder reached four thousand
// machines with no errors at all and established nothing except that the answer
// is higher than four thousand.
//
// So the definition is declared beside the ladder, in the profile, which is the
// only home for the numbers a night is judged by. The run then reports three
// things — the last load that held, the first that did not, and which reading
// said so.

// GaveOut is what counts as the system giving out. Every term is optional and a
// rung crossing any of them has given out; a profile declaring the block states
// at least one.
type GaveOut struct {
	// ErrorRateAbove is work the system refused or dropped. It is the loudest
	// of the three and usually the first to move.
	ErrorRateAbove float64 `yaml:"error_rate_above"`
	// LatencyP95MsAbove is the system still doing the work and taking too long
	// over it, which is the shape a technician actually meets.
	LatencyP95MsAbove float64 `yaml:"latency_p95_ms_above"`
	// TargetBusyPercentAbove is the target out of the processor it was given.
	// It is the term that says the hold-up is the hardware rather than the
	// code, and it is only readable where the run can reach the target's own
	// exposition.
	TargetBusyPercentAbove float64 `yaml:"target_busy_percent_above"`
}

// declared reports whether any term was set.
func (g GaveOut) declared() bool {
	return g.ErrorRateAbove > 0 || g.LatencyP95MsAbove > 0 || g.TargetBusyPercentAbove > 0
}

// BreakingPoint is the ladder's answer.
type BreakingPoint struct {
	// HeldAt and HeldAgents are the last rung that stayed inside every term.
	// Empty when the very first rung gave out, which is itself the finding.
	HeldAt     string `json:"held_at,omitempty"`
	HeldAgents int    `json:"held_agents,omitempty"`
	// GaveAt and GaveAgents are the first rung that did not. Empty when the
	// ladder reached its top still holding, which says the answer is higher
	// than anything this ladder asked for.
	GaveAt     string `json:"gave_at,omitempty"`
	GaveAgents int    `json:"gave_agents,omitempty"`
	// Reason is the reading that decided it, in the words the profile's terms
	// are written in.
	Reason string `json:"reason,omitempty"`
	// RungsRead is how many rungs the search actually looked at.
	//
	// "Nothing gave out" is an answer shaped as an absence, and an absence is
	// satisfied by the absence of the whole conversation — a ladder whose phases
	// never arrived reports the same thing as one that held all the way up. So
	// the count travels, and a bundle carrying an answer that read no rung is
	// refused.
	RungsRead int `json:"rungs_read"`
}

// FindBreakingPoint reads a walk against the definition its profile declared.
//
// A profile that declares none is asking no question and gets no answer rather
// than an empty one.
//
// Only rungs are searched. A ladder ends at the load it started from so the
// report can say whether the system came back, and a phase at or below the level
// before it is that wind-down rather than a step up — reading it as a rung would
// let a recovery phase be the answer to where the climb broke.
func FindBreakingPoint(limits *GaveOut, phases []PhaseResult) *BreakingPoint {
	if limits == nil {
		return nil
	}

	answer := &BreakingPoint{}
	highest := 0
	for _, phase := range phases {
		if phase.OfferedConnectedAgents <= highest {
			continue
		}
		highest = phase.OfferedConnectedAgents
		answer.RungsRead++

		if reason := gaveOutBecause(*limits, phase); reason != "" {
			answer.GaveAt = phase.Name
			answer.GaveAgents = phase.OfferedConnectedAgents
			answer.Reason = reason
			return answer
		}
		answer.HeldAt = phase.Name
		answer.HeldAgents = phase.OfferedConnectedAgents
	}
	return answer
}

// gaveOutBecause is why this rung gave out, or empty while it held.
//
// A term whose reading the run could not take decides nothing: an absent
// busy-ness is a question nobody asked, which is a different thing from a target
// at rest.
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
