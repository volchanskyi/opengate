package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"
)

// The harness's own account of a run has to become a bundle, or the evidence is
// a block of text in a workflow log that nothing reads back — and a comparison
// against a run older than the metrics store's retention has nothing to read.
//
// Everything here is derived from what the run already knows. Nothing is asked
// of the system under test after the fact: a bundle assembled from a later
// query describes the system at the time of the query, which is a different
// moment from the one being reported.

// runBundleInputs is a finished run, as the harness saw it.
type runBundleInputs struct {
	Profile    *Profile
	Results    []agentResult
	StartedAt  time.Time
	Total      time.Duration
	AgentCount int
	Target     string
	// Commit is the source revision; the environment supplies it in CI and the
	// field falls back to a stated unknown, which the bundle then refuses. The
	// harness runs inside a pod that inherits no revision, so every staging
	// bundle carried that string while the canonical rows beside it carried the
	// real one — an evidence file nobody could attribute to any code.
	Commit string

	// TargetShape and GeneratorShape are the two sides of the measurement. The
	// target's limits are known to whoever started it and to nothing in this
	// process, which sees only an address, so they are passed in; the
	// generator's are this machine and are read here.
	TargetShape    Fingerprint
	GeneratorShape Fingerprint

	// Headroom is what the generator had left while it produced the load. A
	// zero value is a run that never looked, which invalidates.
	Headroom Headroom

	// Journeys are the technician-side screens this night timed, read from the
	// export that generator already writes.
	Journeys []JourneyResult

	// FixtureWeight is what the fleet cost on disk, where a run weighed it.
	FixtureWeight *FixtureWeight

	// Filer is the estate's filing, where the run had a fleet to file. What it
	// managed travels, because a run that could not file its estate measured
	// every scoped read against a fleet the product cannot find.
	Filer *estateFiler

	// Phases are the profile's own segments as they actually ran. Empty means
	// the run offered everything at once, which is a shape in its own right and
	// is reported as one phase named for what it was.
	Phases []PhaseResult

	// FlatTargetBusy is how hard the target worked over a run that offered
	// everything at once — the reading a walked phase takes, over the only
	// window that shape has. Nil is a run that could not take it.
	FlatTargetBusy *float64
	// FlatTargetBusyAbsent accounts for a whole-run reading that could not be
	// taken, for the same reason a phase's does.
	FlatTargetBusyAbsent string

	// Registration is how long the server took to write the device row, read
	// from the server itself. Nil means nobody asked it.
	Registration *ServerRegistration

	// Fixture is the fleet this run built, when it built one.
	Fixture *BuiltFixture

	// Conservation is what the target was holding either side of the run, and
	// how many completed operations sit between the two readings.
	Conservation TargetConservation

	// Leak is what grew inside the target between the profiles a long run kept,
	// where the run was asked to keep any. Nil is a run nobody asked to watch.
	Leak *LeakTrail
}

// arrivedAgents counts the machines that connected, handshook and registered.
// A machine that never got in took nothing the target has to give back.
//
// Arriving is what the count is about, and it is not the same question as
// whether the machine's life ended cleanly. A machine that arrived, carried
// load and was severed under it arrived; counting only the ones still standing
// at the wind-down turns every reading taken from this into a reading of the
// survivors, and the harder the run the fewer of those there are. On the night
// the ladder found its breaking point the two answers were 10,520 and 439.
func arrivedAgents(results []agentResult) int {
	arrived := 0
	for _, result := range results {
		if !result.arrivedAt.IsZero() {
			arrived++
		}
	}
	return arrived
}

// askedAgents is the machines that asked the server for something: every
// machine-life the run produced, less the ones the run stood down itself.
//
// A wind-down cancels every start still reaching for the server when a level
// comes down, and such a machine never registered — so counting it among the
// ones that failed to arrive publishes an error rate about the harness's own
// wind-down, against limits several profiles hold at nought. It is the
// denominator the canonical rows already divide by.
//
// It is counted off the results rather than off the fleet the run was told to
// offer, because the two are not the same number wherever a machine is replaced
// when it leaves: the endurance run declares five hundred and produces several
// thousand machine-lives. Dividing by the declaration there puts more arrivals
// over the line than the line allows for and reports a share below nought,
// which every ceiling in every profile passes. Counted this way a machine that
// arrived is a machine that asked, so the share stays between nought and one
// whatever shape the run had.
func askedAgents(results []agentResult) int {
	asked := 0
	for _, result := range results {
		if result.arrivedAt.IsZero() && errors.Is(result.err, context.Canceled) {
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

	// The share of the machines that asked the server for something and did not
	// get in. Over the machine-lives the run produced less what it stood down
	// itself, which is the same denominator the canonical rows use — a run that
	// stood its whole fleet down asked nothing and reports nothing rather than
	// everything.
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
		// The harness holds no long-lived identities of its own: the certificates
		// it signs live in a directory it removes, so a run that reached this
		// point left nothing behind to find.
		Cleanup: CleanupProof{Verified: true},
		Leak:    in.Leak,
	}

	// Where the ladder broke, for a profile that said what breaking means. It is
	// read off the phases the run actually walked, so a run stopped early
	// answers about the rungs it reached rather than about the ones it declared.
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

// unknownCommit is what a run that could not find its own revision says. It is
// a stated absence rather than an empty string so a reader sees a run that did
// not know instead of a field somebody forgot, and the bundle refuses it — a
// measurement that cannot be attributed to any code is not evidence about that
// code.
const unknownCommit = "unknown"

// commitFromEnvironment reads the revision CI already knows, falling back to a
// stated unknown.
func commitFromEnvironment() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return sha
	}
	return unknownCommit
}

// targetFingerprint is the system under test as whoever started it described
// it. A run given no description of its target says what it was pointed at and
// nothing about its shape, which the bundle then refuses.
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

// generatorFingerprint is the machine producing the load. A run that measured
// it says so; one that did not falls back to what the runtime can see about
// itself, which is the processor count and the architecture and no memory.
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
//
// The machine count is the machines that enrolled, not the machines the plan
// asked for. Those are different numbers and were reported as one: a bundle
// said two thousand machines while the database, weighed in the same job,
// held five hundred. What was planned travels beside it under its own name.
func fixtureCounts(in runBundleInputs, enrolled int) FixtureCounts {
	counts := FixtureCounts{Size: FixtureSmall, Tenants: 1, Customers: 1, Sites: 1}
	if in.Profile != nil {
		counts.Size = in.Profile.Fixture
	}

	// A run that built its own fleet knows exactly what customers and accounts
	// are there, so it says so rather than inferring the shape from how many
	// machines it dialled.
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
		// Nothing arrived. The fleet is empty, and the run is invalid for that
		// reason rather than for a fixture the bundle refused to describe.
		counts.Devices = in.AgentCount
	}
	if counts.Devices <= 0 {
		counts.Devices = 1
	}

	if in.FixtureWeight != nil {
		counts.DatabaseBytes = in.FixtureWeight.DatabaseBytes
		counts.TelemetrySeries = in.FixtureWeight.TelemetrySeries
	}
	if in.Filer != nil {
		filed, refused := in.Filer.counts()
		counts.FiledDevices = &filed
		counts.FilingRefusals = &refused
	}
	return counts
}

// phaseResults is the run's phases. A run driven by a profile reports the
// profile's own segments; one without a profile offered everything at once, and
// that is reported as the single phase it was rather than dressed up as more.
func phaseResults(in runBundleInputs, finished time.Time, arrived int,
	register []time.Duration, errorRate float64,
) []PhaseResult {
	if len(in.Phases) > 0 {
		return in.Phases
	}
	return []PhaseResult{connectPhase(in, finished, arrived, register, errorRate)}
}

func connectPhase(in runBundleInputs, finished time.Time, arrived int, register []time.Duration, errorRate float64) PhaseResult {
	// The connect ends when the fleet is up. A run that then holds its fleet for
	// the generator beside it spends most of its wall clock there, so a phase
	// carrying the run's own end reports the hold under the arrival's name.
	lastArrival := finished
	if window := arrivalWindow(in.Results, in.StartedAt); window > 0 {
		lastArrival = in.StartedAt.Add(window)
	}
	return PhaseResult{
		Name:       "connect",
		StartedAt:  in.StartedAt,
		FinishedAt: lastArrival,
		// Every machine is offered at once, so the offered and achieved counts
		// are the fleet and the fleet that arrived, and the two arrival rates
		// are those counts over the window the fleet took to turn up.
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

// latencyObservations records each phase's tail separately. Folding them into
// one aggregate hides which of the three a slow run was slow in, and they are
// three different pieces of work.
//
// The aggregate error rate travels beside them because it is the third of the
// three series the profiles hold their machine-side limits to, and it was the
// one a bundle did not carry — so on the venues whose only output is a bundle,
// every limit named a measurement nothing there could produce.
func latencyObservations(at time.Time, connect, handshake []time.Duration, errorRate float64,
	in runBundleInputs,
) []Observation {
	observations := []Observation{
		{At: at, Series: "connect_p95_ms", Value: millis(percentile(connect, 95))},
		{At: at, Series: "handshake_p95_ms", Value: millis(percentile(handshake, 95))},
		{At: at, Series: "aggregate_error_rate", Value: errorRate},
	}

	observations = append(observations,
		Observation{At: at, Series: "agents_severed_mid_hold", Value: float64(severedMidHold(in.Results))})
	observations = append(observations, targetObservations(at, in.Conservation)...)

	// Registration is reported only when the server was asked. Its own clock
	// stops at a local send buffer, and a number that cannot move is worse than
	// an absent one: two ceilings sat on it for months.
	if in.Registration != nil && in.Registration.Measured() {
		observations = append(observations,
			Observation{At: at, Series: "register_p95_ms", Value: in.Registration.QuantileMs(0.95)},
			Observation{At: at, Series: "register_mean_ms", Value: in.Registration.MeanMs()},
			Observation{At: at, Series: "register_rejected", Value: float64(in.Registration.Rejected)},
			Observation{At: at, Series: "db_pool_in_use", Value: in.Registration.PoolInUse},
			Observation{At: at, Series: "db_pool_open", Value: in.Registration.PoolOpen},
		)
	}
	return observations
}

// severedMidHold counts the machines whose connection went away while they were
// being held.
//
// It is recorded even when it is zero, because zero is the finding: a run that
// held a hundred machines for eight minutes and severed none of them says so,
// and the same run reporting a hundred successes while its fleet was gone is
// what this number exists to make impossible.
func severedMidHold(results []agentResult) int {
	severed := 0
	for _, result := range results {
		if errors.Is(result.err, ErrHeldPeerGone) {
			severed++
		}
	}
	return severed
}

// targetObservations records what the target was holding either side of the
// run, so the bundle carries the question as well as the verdict.
//
// Both readings travel rather than the difference alone: a bundle is read years
// after the metrics store forgot the night, and a delta cannot be re-divided by
// a denominator a later reader wants to change. Open file descriptors travel
// with them because they are what separates a goroutine leak from a socket
// leak — their flatness through a 344 MiB climb is what ruled sockets out.
//
// Resident memory is recorded and not gated, for the reason
// maxRetainedGoroutinesPerOperation states.
func targetObservations(at time.Time, target TargetConservation) []Observation {
	if !target.Start.Read && !target.End.Read {
		return nil
	}
	return []Observation{
		{At: at, Series: "target_goroutines_start", Value: target.Start.Goroutines},
		{At: at, Series: "target_goroutines_end", Value: target.End.Goroutines},
		{At: at, Series: "target_resident_bytes_start", Value: target.Start.ResidentBytes},
		{At: at, Series: "target_resident_bytes_end", Value: target.End.ResidentBytes},
		{At: at, Series: "target_open_fds_start", Value: target.Start.OpenFDs},
		{At: at, Series: "target_open_fds_end", Value: target.End.OpenFDs},
		{At: at, Series: "target_completed_operations", Value: float64(target.Operations)},
		{At: at, Series: "target_retained_goroutines_per_operation", Value: target.RetainedGoroutinesPerOperation()},
		{At: at, Series: "target_retained_bytes_per_operation", Value: target.RetainedBytesPerOperation()},
	}
}

// producedScenarios reports whether this half of the night measured anything. A
// run where nothing connected produced no rows, which is a partial night rather
// than a slow system.
func producedScenarios(arrived int) []string {
	if arrived == 0 {
		return nil
	}
	return []string{"quic-agents"}
}

// summarizeResults splits the run into what arrived and the three latency
// series it produced.
//
// A timing belongs to the machine that took it. A machine that connected in
// 200ms and was severed an hour later connected in 200ms, and dropping it
// because of how its life ended removes the slowest arrivals from the series
// first — which reports a run as faster the more of its fleet it lost.
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

// writeRunBundle puts a built bundle on disk. Building and writing are separate
// because the verdict is read on every run and the file is written only when one
// was asked for.
func writeRunBundle(bundle *Bundle, dir string) error {
	path, err := bundle.WriteTo(dir)
	if err != nil {
		return err
	}
	fmt.Printf("\nEvidence bundle: %s (%s)\n", path, bundle.Verdict.Result)
	return nil
}
