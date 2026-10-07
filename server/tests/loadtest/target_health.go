package main

import (
	"fmt"
	"strings"
	"time"
)

// The four process series a run brackets itself with, and the server's own fleet count.
const (
	goroutinesMetric      = "go_goroutines"
	residentMetric        = "process_resident_memory_bytes"
	openFDsMetric         = "process_open_fds"
	startTimeMetric       = "process_start_time_seconds"
	agentsConnectedMetric = "opengate_agents_connected"
)

// TargetHealth is one reading of the target process.
type TargetHealth struct {
	// Read says the page answered and carried the process families.
	Read bool `json:"read"`

	Goroutines    float64 `json:"goroutines"`
	ResidentBytes float64 `json:"resident_bytes"`
	OpenFDs       float64 `json:"open_fds"`

	// StartTimeSeconds is when this process started; equal values mean the same process.
	StartTimeSeconds float64 `json:"start_time_seconds"`

	// AgentsConnected is the fleet the target reports holding; nil when it publishes no count.
	AgentsConnected *float64 `json:"agents_connected,omitempty"`
}

// TargetConservation is the pair of readings that bracket a run, and the number
// of completed operations between them.
type TargetConservation struct {
	Start      TargetHealth `json:"start"`
	End        TargetHealth `json:"end"`
	Operations int          `json:"operations"`
}

// Bracketed reports whether there is anything to conclude: two readings, and
// work between them to divide by.
func (c TargetConservation) Bracketed() bool {
	return c.Start.Read && c.End.Read && c.Operations > 0
}

// Restarted reports whether the process answering at the end is the one that
// answered at the start.
func (c TargetConservation) Restarted() bool {
	if !c.Start.Read || !c.End.Read {
		return false
	}
	return c.Start.StartTimeSeconds != c.End.StartTimeSeconds
}

// RetainedGoroutinesPerOperation is the goroutine growth one completed operation left behind.
// Subtracting the start reading drops the constant start-up goroutines; a lighter end reads zero.
func (c TargetConservation) RetainedGoroutinesPerOperation() float64 {
	return perOperation(c, c.End.Goroutines-c.Start.Goroutines)
}

// RetainedBytesPerOperation is the same figure for resident memory.
func (c TargetConservation) RetainedBytesPerOperation() float64 {
	return perOperation(c, c.End.ResidentBytes-c.Start.ResidentBytes)
}

func perOperation(c TargetConservation, delta float64) float64 {
	if !c.Bracketed() || delta <= 0 {
		return 0
	}
	return delta / float64(c.Operations)
}

// ParseTargetHealth reads the five families out of an exposition page.
func ParseTargetHealth(page string) TargetHealth {
	var health TargetHealth

	for _, line := range strings.Split(page, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, _, value, ok := splitSample(line)
		if !ok {
			continue
		}
		switch name {
		case goroutinesMetric:
			health.Goroutines = value
			health.Read = true
		case residentMetric:
			health.ResidentBytes = value
			health.Read = true
		case openFDsMetric:
			health.OpenFDs = value
			health.Read = true
		case startTimeMetric:
			health.StartTimeSeconds = value
			health.Read = true
		case agentsConnectedMetric:
			// Only the four process families mark the page read; this one is the product's own.
			held := value
			health.AgentsConnected = &held
		}
	}
	return health
}

// FetchTargetHealth reads the target's own account of itself.
func FetchTargetHealth(baseURL string) (TargetHealth, error) {
	page, err := fetchExpositionPage(baseURL)
	if err != nil {
		return TargetHealth{}, err
	}
	health := ParseTargetHealth(page)
	if !health.Read {
		return TargetHealth{}, fmt.Errorf("read target health: the page carries none of %s, %s, %s, %s",
			goroutinesMetric, residentMetric, openFDsMetric, startTimeMetric)
	}
	return health, nil
}

// readTargetHealth takes one reading; an unreachable page yields an unread TargetHealth.
func readTargetHealth(metricsURL, when string) TargetHealth {
	if metricsURL == "" {
		return TargetHealth{}
	}
	health, err := FetchTargetHealth(metricsURL)
	if err != nil {
		fmt.Printf("::warning::could not read what the target was holding at %s: %v\n", when, err)
		return TargetHealth{}
	}
	return health
}

// settleBudget bounds how long the end reading waits for the target to finish teardown.
const settleBudget = 30 * time.Second

// settleInterval is how often the target is asked during that wait.
const settleInterval = 2 * time.Second

// readSettledTargetHealth takes the end reading once the goroutine count stops falling.
// Connections still closing after the last machine hangs up are waited out for settleBudget.
func readSettledTargetHealth(metricsURL string) TargetHealth {
	if metricsURL == "" {
		return TargetHealth{}
	}

	deadline := time.Now().Add(settleBudget)
	previous := readTargetHealth(metricsURL, "the end of the run")
	for time.Now().Before(deadline) {
		time.Sleep(settleInterval)
		current := readTargetHealth(metricsURL, "the end of the run")
		if !current.Read {
			return previous
		}
		if previous.Read && current.Goroutines >= previous.Goroutines {
			return current
		}
		previous = current
	}
	return previous
}
