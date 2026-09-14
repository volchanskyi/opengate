package main

import (
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/app"
)

// A level a phase publishes is not a level anything held.
//
// `achieved_connected_agents` is `len(running)` over the machines the fleet has
// not wound down, which is bookkeeping the wind-down itself maintains: it
// answers whether the wind-down code ran, and the wind-down code ran. The
// conservation bracket reads the target at the run's start and at its end, so
// every phase between them went unwatched — and two families published a
// recovery figure describing a target still carrying the full fleet on every
// night they ran, with no gate anywhere disagreeing.
//
// These are the rules that disagree, over the target's own account of the same
// population, taken where the phase takes its own.

// minTargetFleetFraction is how much of the fleet a phase claims to be holding
// the target has to be holding too.
//
// The two counts are of one population, kept independently by the two ends, and
// measured against the assembled server they matched exactly: two hundred
// machines held read 200 and five hundred read 500, every sample. So the
// tolerance is not room for the pair to differ — it is room for the interval the
// server's own count is refreshed on, which is app.ProductionGaugeInterval, and
// for a machine leaving inside it.
//
// It is a floor rather than an equality, and only one direction is a finding. A
// target holding fewer machines than the phase claims is a level the harness
// invented; a target holding more is a count that has not yet seen a wind-down
// the harness has already done — every profile ends by standing its fleet down
// inside a phase shorter than the refresh, so an equality here would invalidate
// the drain of every run ever taken.
const minTargetFleetFraction = 0.98

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

// censusRampSteps is how many equal steps a phase climbs across its own length
// in, which is where server/tests/loadtest/sequence.go holds it.
//
// It is here because the last of those steps is how long the phase held the
// level it reports, and that is what decides whether the target's count had
// time to see it.
const censusRampSteps = rampSteps

// censusReasons collects the ways a phase's level was a level nobody held.
//
// The level a phase publishes is `len(running)` over the machines the fleet has
// not wound down, which is bookkeeping the wind-down maintains: it says the
// wind-down code ran. These two put the target's own account beside it — its
// count of the same population, and the goroutines that bound that count below
// — so a phase describing a load the target was not carrying says so.
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

	// The target keeps its count on a timer, so a reading of it describes some
	// instant inside the last interval rather than the instant it was taken.
	// A phase climbs across its whole length in equal steps, so the level it
	// reports is the level it held for the last of them — and where that is
	// shorter than the interval, the count beside it still describes a level
	// the climb has already left. The spike family's spike is thirty seconds,
	// three seconds a step, so judging it here would refuse it for climbing.
	// The pair still travels in the bundle; it is the rule that stands down.
	if held := phase.FinishedAt.Sub(phase.StartedAt) / censusRampSteps; held < app.ProductionGaugeInterval {
		return nil
	}

	var reasons []string
	if float64(*phase.TargetConnectedAgents) < float64(claimed)*minTargetFleetFraction {
		reasons = append(reasons, fmt.Sprintf(
			"phase %q counted %d machines and the target was holding %d (floor %.0f%% of the count), so the level it published is not a load the system carried",
			phase.Name, claimed, *phase.TargetConnectedAgents, minTargetFleetFraction*100))
	}
	if *phase.TargetGoroutines < float64(claimed)*minTargetGoroutinesPerAgent {
		reasons = append(reasons, fmt.Sprintf(
			"phase %q counted %d machines and the target was running %.0f goroutines, below the one per accepted connection its listener starts, so its count has no population behind it",
			phase.Name, claimed, *phase.TargetGoroutines))
	}
	return reasons
}
