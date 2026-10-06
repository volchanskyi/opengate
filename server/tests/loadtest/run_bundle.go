package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
)

// runBundleInputs is a finished run, as the harness saw it.
type runBundleInputs struct {
	Profile    *Profile
	Results    []agentResult
	StartedAt  time.Time
	Total      time.Duration
	AgentCount int
	Target     string
	// Commit is the source revision; empty falls back to the CI environment, then "unknown".
	Commit string

	// TargetShape and GeneratorShape are the two sides of the measurement.
	// The target's limits are passed in; the generator's are read from this machine.
	TargetShape    Fingerprint
	GeneratorShape Fingerprint

	// Headroom is what the generator had left while it produced the load; zero invalidates the run.
	Headroom Headroom

	// Journeys are the technician-side screens this night timed.
	Journeys []JourneyResult

	// FixtureWeight is what the fleet cost on disk, where a run weighed it.
	FixtureWeight *FixtureWeight

	// Filer is the estate's filing, where the run had a fleet to file.
	Filer *estateFiler

	// Phases are the profile's segments as they ran; empty is a run that offered everything at once.
	Phases []PhaseResult

	// FlatTargetBusy is how hard the target worked over a run that offered everything at once.
	// Nil is a run that could not take the reading.
	FlatTargetBusy *float64
	// FlatTargetBusyAbsent accounts for a whole-run reading that could not be taken.
	FlatTargetBusyAbsent string

	// Registration is how long the server took to write the device row; nil means nobody asked.
	Registration *ServerRegistration

	// Fixture is the fleet this run built, when it built one.
	Fixture *BuiltFixture

	// Conservation is what the target held either side of the run and the operations between.
	Conservation TargetConservation

	// Leak is what grew inside the target between the profiles of a long run; nil is a run not watched.
	Leak *LeakTrail
}

// arrivedAgents counts the machines that connected, handshook and registered, whatever
// became of them afterwards.
func arrivedAgents(results []agentResult) int {
	arrived := 0
	for _, result := range results {
		if !result.arrivedAt.IsZero() {
			arrived++
		}
	}
	return arrived
}

// askedAgents counts machine-lives that asked the server for something, leaving out those the
// run stood down itself and those refused on purpose. It counts results, as machines are replaced.
func askedAgents(results []agentResult) int {
	asked := 0
	for _, result := range results {
		if result.arrivedAt.IsZero() && errors.Is(result.err, context.Canceled) {
			continue
		}
		if errors.Is(result.err, ErrEnrollmentRefused) {
			continue
		}
		asked++
	}
	return asked
}

// buildRunBundle turns a finished run into its evidence.
func buildRunBundle(in runBundleInputs) *Bundle {
	arrived, connect, handshake, register := summarizeResults(in.Results)
	finished := in.StartedAt.Add(in.Total)

	// The error rate is the share of asking machines that did not get in; a run that stood
	// its whole fleet down asked nothing and reports zero.
	errorRate := 0.0
	if asked := askedAgents(in.Results); asked > 0 {
		errorRate = float64(asked-arrived) / float64(asked)
	}

	bundle := &Bundle{
		SchemaVersion:     bundleSchemaVersion,
		Run:               runIdentity(in, finished),
		Target:            targetFingerprint(in),
		Generator:         generatorFingerprint(in),
		Fixture:           fixtureCounts(in, arrived),
		Phases:            phaseResults(in, finished, arrived, register, errorRate),
		Journeys:          in.Journeys,
		Observations:      latencyObservations(finished, connect, handshake, errorRate, in),
		GeneratorHeadroom: in.Headroom,
		Cleanup:           uncountedCleanup(in),
		Leak:              in.Leak,
	}

	// The breaking point is read off the phases the run walked, so an early stop answers
	// about the rungs it reached.
	if in.Profile != nil {
		bundle.BreakingPoint = FindBreakingPoint(in.Profile.GaveOut, bundle.Phases)
	}

	bundle.Verdict = Classify(RunInputs{
		Profile:           in.Profile,
		BreakingPoint:     bundle.BreakingPoint,
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: producedScenarios(arrived),
		Headroom:          bundle.GeneratorHeadroom,
		Phases:            bundle.Phases,
		Target:            in.Conservation,
	})

	return bundle
}

func runIdentity(in runBundleInputs, finished time.Time) RunIdentity {
	identity := RunIdentity{
		ID:             fmt.Sprintf("quic-agents-%d", in.StartedAt.UTC().Unix()),
		Commit:         in.Commit,
		ProfileName:    "ad-hoc",
		ProfileVersion: profileSchemaVersion,
		Family:         FamilyNormal,
		Environment:    EnvStaging,
		StartedAt:      in.StartedAt,
		FinishedAt:     finished,
	}
	if in.Commit == "" {
		identity.Commit = commitFromEnvironment()
	}
	if in.Profile != nil {
		identity.ProfileName = in.Profile.Name
		identity.ProfileVersion = in.Profile.SchemaVersion
		identity.Family = in.Profile.Family
		identity.Environment = in.Profile.Environment
	}
	return identity
}

// unknownCommit is a stated absence for a run that cannot find its revision; the bundle refuses it.
const unknownCommit = "unknown"

// commitFromEnvironment reads the revision CI already knows, falling back to unknownCommit.
func commitFromEnvironment() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return sha
	}
	return unknownCommit
}

// uncountedCleanup is the bundle's cleanup section as the harness can write it: uncounted, with
// the reason, since removal happens after the harness has finished.
func uncountedCleanup(in runBundleInputs) CleanupProof {
	if in.Profile != nil && in.Profile.Environment == EnvRunner {
		return CleanupProof{NotCounted: "the stack is torn down with the job that built it, so nothing outlives the run to count"}
	}
	return CleanupProof{NotCounted: "the cleanup step counts what the run left and folds its proof into this bundle after the run"}
}

// targetFingerprint is the system under test as whoever started it described it.
func targetFingerprint(in runBundleInputs) Fingerprint {
	shape := in.TargetShape
	if shape.Kind == "" {
		shape.Kind = "system-under-test"
	}
	if shape.Description == "" {
		shape.Description = in.Target
	}
	return shape
}

// generatorFingerprint is the machine producing the load, falling back to the processor count
// and architecture the runtime reports.
func generatorFingerprint(in runBundleInputs) Fingerprint {
	shape := in.GeneratorShape
	if shape.Kind == "" {
		shape.Kind = "quic-harness"
	}
	if shape.Description == "" {
		shape.Description = "server/tests/loadtest"
	}
	if shape.CPUs <= 0 {
		shape.CPUs = float64(runtime.NumCPU())
	}
	if shape.Arch == "" {
		shape.Arch = runtime.GOARCH
	}
	return shape
}

// fixtureCounts records the fleet this run drove.
// Devices is the machines that enrolled; the planned count travels beside it.
func fixtureCounts(in runBundleInputs, enrolled int) FixtureCounts {
	counts := FixtureCounts{Size: FixtureSmall, Tenants: 1, Customers: 1, Sites: 1}
	if in.Profile != nil {
		counts.Size = in.Profile.Fixture
	}

	if in.Fixture != nil {
		built := in.Fixture.Counts()
		counts.Size = built.Size
		counts.Tenants = built.Tenants
		counts.Customers = built.Customers
		counts.Sites = built.Sites
		counts.Users = built.Users
		counts.PlannedDevices = built.PlannedDevices
	}

	counts.Devices = enrolled
	if counts.Devices <= 0 && in.AgentCount > 0 {
		// Nothing arrived, so the fleet is empty and the run is invalid for that reason.
		counts.Devices = in.AgentCount
	}
	if counts.Devices <= 0 {
		counts.Devices = 1
	}

	if in.FixtureWeight != nil {
		counts.DatabaseBytes = in.FixtureWeight.DatabaseBytes
		counts.TelemetrySeries = in.FixtureWeight.Counts.TelemetrySeries
	}
	if in.Filer != nil {
		filed, refused := in.Filer.counts()
		counts.FiledDevices = &filed
		counts.FilingRefusals = &refused
	}
	return counts
}

// phaseResults is the run's phases: the profile's own segments, or a single connect phase for a
// run that offered everything at once.
func phaseResults(in runBundleInputs, finished time.Time, arrived int,
	register []time.Duration, errorRate float64,
) []PhaseResult {
	if len(in.Phases) > 0 {
		return in.Phases
	}
	return []PhaseResult{connectPhase(in, finished, arrived, register, errorRate)}
}

func connectPhase(in runBundleInputs, finished time.Time, arrived int, register []time.Duration, errorRate float64) PhaseResult {
	// The connect phase ends when the fleet is up, so the hold afterwards is not reported as arrival.
	lastArrival := finished
	if window := arrivalWindow(in.Results, in.StartedAt); window > 0 {
		lastArrival = in.StartedAt.Add(window)
	}
	return PhaseResult{
		Name:       "connect",
		StartedAt:  in.StartedAt,
		FinishedAt: lastArrival,
		// Every machine is offered at once, so the arrival rates are fleet counts over the window.
		OfferedAgentArrivalsPerSecond:  ratePerSecond(int64(in.AgentCount), lastArrival.Sub(in.StartedAt).Seconds()),
		AchievedAgentArrivalsPerSecond: ratePerSecond(int64(arrived), lastArrival.Sub(in.StartedAt).Seconds()),
		OfferedConnectedAgents:         in.AgentCount,
		AchievedConnectedAgents:        arrived,
		LatencyP50Ms:                   millis(percentile(register, 50)),
		LatencyP95Ms:                   millis(percentile(register, 95)),
		LatencyP99Ms:                   millis(percentile(register, 99)),
		ErrorRate:                      errorRate,
		TargetBusyPercent:              in.FlatTargetBusy,
		TargetBusyAbsent:               in.FlatTargetBusyAbsent,
	}
}

// producedScenarios names the scenario this half of the night measured, and none when nothing
// connected, which makes the night partial.
func producedScenarios(arrived int) []string {
	if arrived == 0 {
		return nil
	}
	return []string{"quic-agents"}
}

// summarizeResults splits the run into the arrival count and the three latency series.
// A timing belongs to the machine that took it, whatever became of that machine later.
func summarizeResults(results []agentResult) (arrived int, connect, handshake, register []time.Duration) {
	for _, result := range results {
		if result.arrivedAt.IsZero() {
			continue
		}
		arrived++
		connect = append(connect, result.connectDur)
		handshake = append(handshake, result.handshakeDur)
		register = append(register, result.registerDur)
	}
	return arrived, connect, handshake, register
}

func millis(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// writeRunBundle puts a built bundle on disk when a run asked for the file.
func writeRunBundle(bundle *Bundle, dir string) error {
	path, err := bundle.WriteTo(dir)
	if err != nil {
		return err
	}
	fmt.Printf("\nEvidence bundle: %s (%s)\n", path, bundle.Verdict.Result)
	return nil
}
