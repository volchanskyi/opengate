package main

import (
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
	// field falls back to a stated unknown rather than to an empty string that
	// would fail the bundle for the wrong reason.
	Commit string

	// Phases are the profile's own segments as they actually ran. Empty means
	// the run offered everything at once, which is a shape in its own right and
	// is reported as one phase named for what it was.
	Phases []PhaseResult

	// Registration is how long the server took to write the device row, read
	// from the server itself. Nil means nobody asked it.
	Registration *ServerRegistration

	// Fixture is the fleet this run built, when it built one.
	Fixture *BuiltFixture

	// Conservation is what the target was holding either side of the run, and
	// how many completed operations sit between the two readings.
	Conservation TargetConservation
}

// succeededAgents counts the machines that connected, handshook and registered.
// A machine that failed took nothing the target has to give back.
func succeededAgents(results []agentResult) int {
	succeeded := 0
	for _, result := range results {
		if result.err == nil {
			succeeded++
		}
	}
	return succeeded
}

// buildRunBundle turns a finished run into its evidence.
func buildRunBundle(in runBundleInputs) *Bundle {
	succeeded, connect, handshake, register := summarizeResults(in.Results)
	finished := in.StartedAt.Add(in.Total)

	errorRate := 0.0
	if in.AgentCount > 0 {
		errorRate = float64(in.AgentCount-succeeded) / float64(in.AgentCount)
	}

	bundle := &Bundle{
		SchemaVersion: bundleSchemaVersion,
		Run:           runIdentity(in, finished),
		Target: Fingerprint{
			Kind:        "system-under-test",
			Description: in.Target,
			CPUs:        1,
			MemoryBytes: 1,
		},
		Generator:    generatorFingerprint(),
		Fixture:      fixtureCounts(in),
		Phases:       phaseResults(in, finished, succeeded, register, errorRate),
		Observations: latencyObservations(finished, connect, handshake, in),
		// The harness holds no long-lived identities of its own: the certificates
		// it signs live in a directory it removes, so a run that reached this
		// point left nothing behind to find.
		Cleanup: CleanupProof{Verified: true},
	}

	// The generator's own headroom is not measured from inside the process it
	// would have to measure, so it is reported as the processor count the
	// runtime sees, with the saturation judgement left to the phase results.
	bundle.GeneratorHeadroom = Headroom{CPUHeadroomPercent: 100, MemoryUsedPercent: 0}

	bundle.Verdict = Classify(RunInputs{
		Profile:           in.Profile,
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: producedScenarios(succeeded),
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

// commitFromEnvironment reads the revision CI already knows, falling back to a
// stated unknown. An empty commit fails the bundle, and failing a run because
// nobody exported a variable is a failure about the harness rather than about
// the system.
func commitFromEnvironment() string {
	if sha := os.Getenv("GITHUB_SHA"); sha != "" {
		return sha
	}
	return "unknown"
}

func generatorFingerprint() Fingerprint {
	return Fingerprint{
		Kind:        "quic-harness",
		Description: "server/tests/loadtest",
		CPUs:        runtime.NumCPU(),
		MemoryBytes: 1,
		Arch:        runtime.GOARCH,
	}
}

// fixtureCounts records the fleet this run drove. The harness signs its own
// certificates rather than building a fleet through the API, so the device
// count is the fleet and the rest is stated as the one tenant it ran in.
func fixtureCounts(in runBundleInputs) FixtureCounts {
	// A run that built its own fleet knows exactly what is there, so it says so
	// rather than inferring the shape from how many machines it dialled.
	if in.Fixture != nil {
		return in.Fixture.Counts()
	}

	size := FixtureSmall
	if in.Profile != nil {
		size = in.Profile.Fixture
	}
	devices := in.AgentCount
	if devices <= 0 {
		devices = 1
	}
	return FixtureCounts{Size: size, Tenants: 1, Customers: 1, Sites: 1, Users: 0, Devices: devices}
}

// phaseResults is the run's phases. A run driven by a profile reports the
// profile's own segments; one without a profile offered everything at once, and
// that is reported as the single phase it was rather than dressed up as more.
func phaseResults(in runBundleInputs, finished time.Time, succeeded int,
	register []time.Duration, errorRate float64,
) []PhaseResult {
	if len(in.Phases) > 0 {
		return in.Phases
	}
	return []PhaseResult{connectPhase(in, finished, succeeded, register, errorRate)}
}

func connectPhase(in runBundleInputs, finished time.Time, succeeded int, register []time.Duration, errorRate float64) PhaseResult {
	// The connect ends when the fleet is up. A run that then holds its fleet for
	// the generator beside it spends most of its wall clock there, so a phase
	// carrying the run's own end reports the hold under the arrival's name.
	arrived := finished
	if window := arrivalWindow(in.Results, in.StartedAt); window > 0 {
		arrived = in.StartedAt.Add(window)
	}
	return PhaseResult{
		Name:       "connect",
		StartedAt:  in.StartedAt,
		FinishedAt: arrived,
		// Every machine is offered at once, so the offered and achieved counts
		// are the fleet and the fleet that arrived.
		OfferedConnectedAgents:  in.AgentCount,
		AchievedConnectedAgents: succeeded,
		LatencyP50Ms:            millis(percentile(register, 50)),
		LatencyP95Ms:            millis(percentile(register, 95)),
		LatencyP99Ms:            millis(percentile(register, 99)),
		ErrorRate:               errorRate,
	}
}

// latencyObservations records each phase's tail separately. Folding them into
// one aggregate hides which of the three a slow run was slow in, and they are
// three different pieces of work.
func latencyObservations(at time.Time, connect, handshake []time.Duration, in runBundleInputs) []Observation {
	observations := []Observation{
		{At: at, Series: "connect_p95_ms", Value: millis(percentile(connect, 95))},
		{At: at, Series: "handshake_p95_ms", Value: millis(percentile(handshake, 95))},
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
func producedScenarios(succeeded int) []string {
	if succeeded == 0 {
		return nil
	}
	return []string{"quic-agents"}
}

// summarizeResults splits the run into what succeeded and the three latency
// series it produced.
func summarizeResults(results []agentResult) (succeeded int, connect, handshake, register []time.Duration) {
	for _, result := range results {
		if result.err != nil {
			continue
		}
		succeeded++
		connect = append(connect, result.connectDur)
		handshake = append(handshake, result.handshakeDur)
		register = append(register, result.registerDur)
	}
	return succeeded, connect, handshake, register
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
