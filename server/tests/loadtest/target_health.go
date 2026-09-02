package main

import (
	"fmt"
	"strings"
	"time"
)

// What the target was holding, read off the page the harness already fetches.
//
// A run reports what it drove and what came back. It did not report what state
// it left the system in, and a night where the target was replaced underneath
// the generator produced a bundle that looked like every other night: the
// scenarios ran, the rows arrived, and the verdict was computed over numbers
// measured against two different processes.
//
// The four families below are the ones that answer it, and every one of them is
// already on the page this harness reads registration timing from — the
// registry registers the client library's Go and process collectors. Nothing
// here needs a kubeconfig, a pod UID or a restart count, which is what lets the
// same reading work for the volume and scaling families, whose target runs in a
// compose stack on a runner where there is no cluster to ask.
//
// They are also the only numbers in the exposition that are readings rather
// than bookkeeping. Every opengate_* series is maintained by the code path it
// describes, so it says the teardown ran; these say whether the resource came
// back.

// The four series a run brackets itself with.
const (
	goroutinesMetric = "go_goroutines"
	residentMetric   = "process_resident_memory_bytes"
	openFDsMetric    = "process_open_fds"
	startTimeMetric  = "process_start_time_seconds"
)

// TargetHealth is one reading of the target process.
type TargetHealth struct {
	// Read says the page answered and carried the families. An unread page is
	// not a reading of zero: zero goroutines and zero bytes is the healthiest
	// figure a process could report, so recording an unanswered probe as one
	// would turn a target nobody could reach into the best target ever measured.
	Read bool `json:"read"`

	Goroutines    float64 `json:"goroutines"`
	ResidentBytes float64 `json:"resident_bytes"`
	OpenFDs       float64 `json:"open_fds"`

	// StartTimeSeconds is when this process started. It is the only field that
	// says whether two readings came from the same process, which is what makes
	// every other number between them comparable.
	StartTimeSeconds float64 `json:"start_time_seconds"`
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

// RetainedGoroutinesPerOperation is what one completed operation cost the
// target and never gave back.
//
// Per operation rather than absolute, because an absolute figure has to guess
// at a floor: the server, its store and its pool start goroutines that take no
// context and never stop, and the number of them changes whenever anything else
// in the process does. Dividing by the work removes that constant and states
// the property directly — a completed operation gives back what it took.
//
// A target that ended lighter than it started retained nothing. It is not a
// credit against a later run.
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

// ParseTargetHealth reads the four families out of an exposition page.
//
// It reads only those, the way the registration reader beside it does, so what
// the harness depends on is visible in one place rather than behind a parser
// for the whole format.
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

// readTargetHealth takes one reading, reporting an unread target rather than
// failing the run. A harness that could not reach the page has measured
// nothing about the target, and says so in the bundle.
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

// settleBudget bounds how long the end reading waits for the target to finish
// putting things back.
const settleBudget = 30 * time.Second

// settleInterval is how often the target is asked during that wait.
const settleInterval = 2 * time.Second

// readSettledTargetHealth takes the end reading once the target has stopped
// giving things back.
//
// Teardown is not instantaneous on the far side of a network. A reading taken
// the moment the last machine hangs up counts connections that are still
// closing, and would report them as retained — a red run about the harness's
// own impatience rather than about the system. So the target is asked until its
// goroutine count stops falling, or until the budget runs out, and the last
// reading is the one recorded. A target that is still shedding at the deadline
// is reported as it is: waiting longer for a number to improve is how a gate
// stops being one.
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
