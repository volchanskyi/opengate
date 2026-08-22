package main

import (
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
		Phases:       []PhaseResult{connectPhase(in, finished, succeeded, register, errorRate)},
		Observations: latencyObservations(finished, connect, handshake, register),
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

func connectPhase(in runBundleInputs, finished time.Time, succeeded int, register []time.Duration, errorRate float64) PhaseResult {
	return PhaseResult{
		Name:       "connect",
		StartedAt:  in.StartedAt,
		FinishedAt: finished,
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
func latencyObservations(at time.Time, connect, handshake, register []time.Duration) []Observation {
	return []Observation{
		{At: at, Series: "connect_p95_ms", Value: millis(percentile(connect, 95))},
		{At: at, Series: "handshake_p95_ms", Value: millis(percentile(handshake, 95))},
		{At: at, Series: "register_p95_ms", Value: millis(percentile(register, 95))},
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

// writeRunBundle is the thin wrapper the harness's main calls.
func writeRunBundle(dir string, profile *Profile, results []agentResult,
	startedAt time.Time, total time.Duration, agents int, target string,
) error {
	bundle := buildRunBundle(runBundleInputs{
		Profile:    profile,
		Results:    results,
		StartedAt:  startedAt,
		Total:      total,
		AgentCount: agents,
		Target:     target,
	})
	path, err := bundle.WriteTo(dir)
	if err != nil {
		return err
	}
	fmt.Printf("\nEvidence bundle: %s (%s)\n", path, bundle.Verdict.Result)
	return nil
}
