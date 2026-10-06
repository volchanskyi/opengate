package main

import (
	"fmt"
	"strings"
	"time"
)

// targetCPUMetric is the family the target publishes its own processor time in.
const targetCPUMetric = "process_cpu_seconds_total"

// Reasons a phase carries no busy-ness reading; a phase naming one keeps its place in the bundle.
const (
	busyAbsentTargetSilent    = "the target did not answer its own exposition"
	busyAbsentNoAllowance     = "nobody declared what the target was capped at"
	busyAbsentTargetRestarted = "the target restarted inside the phase"
	busyAbsentNoWindow        = "the phase had no length to divide the work by"
)

// TargetBusy reads what share of its processor allowance the target used in a phase.
type TargetBusy struct {
	// ReadCPUSeconds returns the target's cumulative processor seconds and whether they were read.
	ReadCPUSeconds func() (float64, bool)
	// Allowance is the processor share the target is capped at, the figure its fingerprint carries.
	Allowance float64
}

// Bracket takes the opening reading and returns a closer that divides the work by the given window.
// The closer returns the busy percent or, where there is none, the reason; no target gives neither.
func (b TargetBusy) Bracket() func(window time.Duration) (*float64, string) {
	if b.ReadCPUSeconds == nil {
		return func(time.Duration) (*float64, string) { return nil, "" }
	}

	before, opened := b.read()

	return func(window time.Duration) (*float64, string) {
		after, closed := b.read()
		if !opened || !closed {
			return nil, busyAbsentTargetSilent
		}
		if b.Allowance <= 0 {
			return nil, busyAbsentNoAllowance
		}
		if window <= 0 {
			return nil, busyAbsentNoWindow
		}

		// A counter that went backwards means the target restarted inside the phase.
		used := after - before
		if used < 0 {
			return nil, busyAbsentTargetRestarted
		}

		percent := used / window.Seconds() / b.Allowance * 100
		return &percent, ""
	}
}

func (b TargetBusy) read() (float64, bool) {
	if b.ReadCPUSeconds == nil {
		return 0, false
	}
	return b.ReadCPUSeconds()
}

// NewTargetBusy reads a live target; an empty address reads nothing.
func NewTargetBusy(metricsURL string, allowance float64) TargetBusy {
	if metricsURL == "" {
		return TargetBusy{}
	}
	return TargetBusy{
		ReadCPUSeconds: func() (float64, bool) {
			return FetchTargetCPUSeconds(metricsURL)
		},
		Allowance: allowance,
	}
}

// FetchTargetCPUSeconds reads the processor time the target has used since it started.
func FetchTargetCPUSeconds(baseURL string) (float64, bool) {
	page, err := fetchExpositionPage(baseURL)
	if err != nil {
		fmt.Printf("::warning::could not read how hard the target was working: %v\n", err)
		return 0, false
	}
	return ParseTargetCPUSeconds(page)
}

// ParseTargetCPUSeconds reads the processor counter out of an exposition page.
func ParseTargetCPUSeconds(page string) (float64, bool) {
	for _, line := range strings.Split(page, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, value, ok := splitSample(line)
		if ok && name == targetCPUMetric {
			return value, true
		}
	}
	return 0, false
}
