// Package main is a QUIC agent load harness reporting mTLS handshake and registration timing.
//
// Usage:
//
//	go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090 -data-dir=/tmp/loadtest
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"sync/atomic"
	"time"
)

// agentDeadline bounds connecting, handshaking and registering; the hold is added to it.
const agentDeadline = 30 * time.Second

const (
	// exitAgentFailures marks a measured run in which some machines did not arrive; the runner
	// keeps its output.
	exitAgentFailures = 1
	// exitMeasuredNothing marks a run that measured nothing; the runner discards its output.
	exitMeasuredNothing = 2
)

type agentResult struct {
	connectDur   time.Duration
	handshakeDur time.Duration
	registerDur  time.Duration

	// arrivedAt is when the machine finished registering; it bounds the arrival window, and an
	// end time would measure the hold.
	arrivedAt time.Time

	// redials counts dials after a lost connection and reconnected the ones that succeeded.
	redials     int
	reconnected int

	err error
}

// exitCode checks the verdict before the failure count, so a run that measured nothing is never
// reported as one that did.
func exitCode(verdict Verdict, failures int, answer *BreakingPoint) int {
	switch {
	case verdict.Result == ResultInvalid:
		return exitMeasuredNothing
	case verdict.Result == ResultFailed:
		return exitAgentFailures
	case failures > 0 && !ladderFoundItsAnswer(answer):
		return exitAgentFailures
	default:
		return 0
	}
}

// ladderFoundItsAnswer reports a capacity ladder that reached its breaking point, whose lost
// machines are its result.
func ladderFoundItsAnswer(answer *BreakingPoint) bool {
	return answer != nil && answer.GaveAt != ""
}

func main() {
	os.Exit(run())
}

// run returns the exit code so the deferred certificate-directory removal runs on every path.
func run() int {
	agents := flag.Int("agents", 100, "number of concurrent agents")
	addr := flag.String("addr", "127.0.0.1:9090", "QUIC server address")
	dataDir := flag.String("data-dir", "", "cert manager data directory (temp if empty)")
	tenantFlag := flag.Int("tenants", 1, "number of tenant cohorts to spread agents across")
	defaultTelemetry := flag.Bool("default-telemetry", false, "emit the default telemetry shape (health summary + host metric window + process report) per agent")
	telemetryCycles := flag.Int("telemetry-cycles", 1, "default-telemetry emission cycles per agent")
	metricWindows := flag.Int("metric-windows", 0, "extra host-metric windows each agent emits after register")
	answerLogPulls := flag.Bool("answer-log-pulls", false, "answer one on-demand raw-log pull per agent")
	backfillBatches := flag.Int("backfill-batches", 0, "reconnect-storm backfill batches each agent drains after register")
	backfillSamples := flag.Int("backfill-samples", 100, "pre-rolled samples per backfill batch")
	holdFor := flag.Duration("hold", 0, "keep every agent connected for this long after its traffic, so a generator on the other side has machines to open sessions against")
	reconnect := flag.Bool("reconnect", false, "keep a machine in the run after its connection breaks, so a fleet behind a link that goes dark is still there when the link returns; off measures the server and reports a severance instead of repairing it")
	retryDeferred := flag.Bool("retry-deferred", false, "ask again when the server tells a machine to wait for a catch-up slot, as a shipped agent does; off sheds the load so the deferral path is measured rather than queued through")
	hostnamePrefix := flag.String("hostname-prefix", defaultHostnamePrefix, "the name this run's machines carry, so a run can count and remove its own")
	relaySessions := flag.Bool("relay-sessions", false, "answer SessionRequest by joining the machine side of the relay and echoing, so the browser side can time a real round trip")
	profilePath := flag.String("profile", "", "load/profiles/<name>.yaml declaring the phases, safety limits and gates")
	bundleDir := flag.String("bundle", "", "directory to write this run's evidence bundle into")
	enrollURL := flag.String("enroll-url", "", "server base URL to enroll each agent through, so no certificate authority key leaves the cluster")
	enrollToken := flag.String("enroll-token", "", "enrollment token to spend, minted through the admin API before the run")
	metricsURL := flag.String("metrics-url", "", "server base URL to read registration timing from, so the figure is the one the server measured where the device row landed rather than this process's own send buffer")
	targetNetCounters := flag.String("target-net-counters", "", "the target's kernel network counters page (/proc/<pid>/net/snmp of the server process), readable where the target shares this machine's kernel, so each phase says what the target's end dropped")
	fixtureAccount := flag.String("fixture-account", "", "administrator to build the fixture as; empty builds no fixture")
	fixturePasswordFlag := flag.String("fixture-password", "", "that administrator's password")
	fixtureSize := flag.String("fixture-size", "", "fleet to build before the run: small, large or lopsided; empty takes the profile's own")
	fixtureSeed := flag.Uint64("fixture-seed", 1, "the seed the fleet is derived from, so the same seed reproduces the same fleet")
	fixtureBootstrap := flag.Bool("fixture-bootstrap", false, "the environment starts empty, so register the administrator instead of signing in as one")
	targetCPUs := flag.String("target-cpus", "", "the processor share the system under test is capped at, which this process cannot see and whoever started it knows")
	targetMemory := flag.String("target-memory-bytes", "", "the memory the system under test is capped at, in bytes")
	targetDescription := flag.String("target-description", "", "what the system under test is, in words a later reader can interpret the run by")
	commit := flag.String("commit", "", "the source revision this run measures; a run inside a pod inherits none, so it is passed in")
	journeysPath := flag.String("journeys", "", "the technician-side generator's export, whose named journeys travel into this run's evidence")
	fixtureWeightPath := flag.String("fixture-weight", "", "the weighing of the fleet on disk, which is the volume family's whole finding")
	leakSnapshotEvery := flag.Duration("leak-snapshot-every", 0, "take and keep the target's goroutine and heap profiles this often, so what grew across a long run is named at a line rather than reported as a slope")
	flag.Parse()

	// Production is never a target, so the address is checked before anything dials.
	if err := CheckQUICAddress(*addr); err != nil {
		log.Fatalf("refusing to run: %v", err)
	}

	var profile *Profile
	if *profilePath != "" {
		var err error
		if profile, err = LoadProfile(*profilePath); err != nil {
			log.Fatalf("profile: %v", err)
		}
	}

	// sessionsJoined counts the machine sides answered across every agent, the conservation
	// denominator.
	sessionsJoined := &atomic.Int64{}

	opts := loadOptions{
		sessionsJoined:          sessionsJoined,
		defaultTelemetry:        *defaultTelemetry,
		telemetryCycles:         *telemetryCycles,
		metricWindows:           *metricWindows,
		answerLogPulls:          *answerLogPulls,
		backfillBatches:         *backfillBatches,
		backfillSamplesPerBatch: *backfillSamples,
		holdFor:                 *holdFor,
		reconnect:               *reconnect,
		retryDeferred:           *retryDeferred,
		relaySessions:           *relaySessions,
	}

	tenants := max(*tenantFlag, 1)
	agentPlan := planAgents(*agents, tenants, *hostnamePrefix)

	dir := *dataDir
	if dir == "" && *enrollURL == "" {
		var err error
		dir, err = os.MkdirTemp("", "loadtest-certs-*")
		if err != nil {
			log.Fatalf("create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
	}

	// The fleet is built before the clock starts so its writes stay out of every phase.
	fleet := buildFixtureIfAsked(fixtureRequest{
		baseURL:   *enrollURL,
		account:   *fixtureAccount,
		password:  *fixturePasswordFlag,
		size:      *fixtureSize,
		seed:      *fixtureSeed,
		profile:   profile,
		bootstrap: *fixtureBootstrap,
		runFor:    loadLastsFor(profile, *holdFor),
	})
	fixture := fleet.fixture
	if fleet.token != "" {
		*enrollToken = fleet.token
	}

	source, err := newAgentCredentials(dir, *enrollURL, *enrollToken)
	if err != nil {
		log.Fatalf("agent credentials: %v", err)
	}
	// enrolOnce mints each machine's identity once; the fleet dials with it and the filer reads
	// its identifier.
	credentials := enrolOnce(source)
	filer := fleet.filerFor(credentials, *agents, profile)

	fmt.Printf("Starting QUIC load test: %d agents across %d tenant(s) → %s\n", *agents, tenants, *addr)

	// The conservation bracket opens after the fixture so its writes are not charged to the
	// target's per-operation figure.
	targetAtStart := readTargetHealth(*metricsURL, "the start of the run")

	// The leak watch opens after the fixture, so the first reading excludes its writes.
	leakWatching := WatchForLeaks(*metricsURL, *bundleDir, *leakSnapshotEvery)

	start := time.Now()

	generatorReading := WatchGenerator()

	// The cap and the busy-ness denominator share one declaration, so the bundle cannot
	// disagree with itself.
	targetShape := ParseFingerprintFlags("system-under-test", *targetDescription, *targetCPUs, *targetMemory)

	results, phases, flatBusy, flatBusyAbsent := runWorkload(profile, *agents, agentPlan, credentials, *addr, opts,
		PhaseReadings{
			Busy:      NewTargetBusy(*metricsURL, targetShape.CPUs),
			Census:    NewTargetCensus(*metricsURL),
			Generator: NewGeneratorRoom(),
			Network:   NewNetworkDrops(*targetNetCounters),
		}, filer)
	totalDur := time.Since(start)

	generatorHeadroom := generatorReading()

	targetAtEnd := readSettledTargetHealth(*metricsURL)

	// The closing reading follows the target's wind-down, so retained means what survived it.
	leakTrail := leakWatching()

	// Registration is read from the server before the results block, which publishes it.
	registration := readServerRegistration(*metricsURL)

	failures := reportResults(results, start, totalDur, *agents, registration)

	// The bundle is built whether or not it is written because it carries the verdict.
	bundle := buildRunBundle(runBundleInputs{
		Profile:    profile,
		Results:    results,
		StartedAt:  start,
		Total:      totalDur,
		AgentCount: *agents,
		Target:     *addr,
		Commit:     *commit,
		// The target's limits come from whoever started it; the generator's are read here.
		TargetShape:          targetShape,
		GeneratorShape:       ReadGeneratorShape("server/tests/loadtest"),
		Headroom:             generatorHeadroom,
		Journeys:             readJourneys(*journeysPath),
		FixtureWeight:        readFixtureWeight(*fixtureWeightPath),
		Filer:                filer,
		Phases:               phases,
		FlatTargetBusy:       flatBusy,
		FlatTargetBusyAbsent: flatBusyAbsent,
		Registration:         registration,
		Fixture:              fixture,
		Leak:                 leakTrail,
		Conservation: TargetConservation{
			Start: targetAtStart,
			End:   targetAtEnd,
			// Each connected machine and answered session is one operation the target must give back.
			Operations: arrivedAgents(results) + int(sessionsJoined.Load()),
		},
	})

	if *bundleDir != "" {
		if err := writeRunBundle(bundle, *bundleDir); err != nil {
			log.Fatalf("bundle: %v", err)
		}
	}

	printBreakingPoint(bundle.BreakingPoint)
	printLeakTrail(bundle.Leak)

	for _, reason := range bundle.Verdict.Reasons {
		fmt.Printf("::error::%s\n", reason)
	}
	return exitCode(bundle.Verdict, failures, bundle.BreakingPoint)
}

// arrivalWindow runs from the start to the last successful registration; the wall clock would
// include the hold.
func arrivalWindow(results []agentResult, start time.Time) time.Duration {
	var window time.Duration
	for _, r := range results {
		if r.err != nil || r.arrivedAt.IsZero() {
			continue
		}
		if elapsed := r.arrivedAt.Sub(start); elapsed > window {
			window = elapsed
		}
	}
	return window
}

// reportResults prints the timing summary and returns the number of failed agents.
func reportResults(results []agentResult, start time.Time, totalDur time.Duration, agents int,
	registration *ServerRegistration,
) int {
	var (
		successes    int
		failures     int
		stoodDown    int
		connectTimes []time.Duration
		hsTimes      []time.Duration
	)
	for _, r := range results {
		switch {
		case !r.arrivedAt.IsZero():
			// A machine that arrived is a success even if its connection later broke; that is a
			// severance, counted elsewhere.
			successes++
			connectTimes = append(connectTimes, r.connectDur)
			hsTimes = append(hsTimes, r.handshakeDur)
		case errors.Is(r.err, context.Canceled):
			// The run stood this machine down before it registered; see FleetOutcomes.StoodDown.
			stoodDown++
		default:
			failures++
		}
	}

	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Total time:  %s\n", totalDur.Round(time.Millisecond))
	fmt.Printf("Arrival window:  %s\n", arrivalWindow(results, start).Round(time.Millisecond))
	fmt.Printf("Agents:      %d/%d succeeded\n", successes, agents)
	fmt.Printf("Failures:    %d\n", failures)
	if stoodDown > 0 {
		fmt.Printf("Stood down:  %d\n", stoodDown)
	}

	if successes > 0 {
		// Connect and handshake are visible only to the generator.
		fmt.Printf("\nConnect:     p50=%s  p95=%s  p99=%s\n",
			percentile(connectTimes, 50), percentile(connectTimes, 95), percentile(connectTimes, 99))
		fmt.Printf("Handshake:   p50=%s  p95=%s  p99=%s\n",
			percentile(hsTimes, 50), percentile(hsTimes, 95), percentile(hsTimes, 99))
		printRegisterLine(registration)
	}

	if failures > 0 {
		printErrorSamples(results)
	}
	return failures
}

// printRegisterLine prints the server-measured registration timing, or nothing when the server
// gave none; the local clock stops at a send buffer.
func printRegisterLine(registration *ServerRegistration) {
	if registration == nil || !registration.Measured() {
		return
	}
	fmt.Printf("Register:    p50=%s  p95=%s  p99=%s\n",
		millisDuration(registration.QuantileMs(0.50)),
		millisDuration(registration.QuantileMs(0.95)),
		millisDuration(registration.QuantileMs(0.99)))
}

// millisDuration uses the duration form of the percentile lines so one parser reads the block.
func millisDuration(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond)).Round(time.Microsecond)
}

// printErrorSamples groups failures by kind, commonest first; whole messages name their machine
// and are unique.
func printErrorSamples(results []agentResult) {
	byKind := map[string]int{}
	sample := map[string]string{}
	failures := 0
	for _, r := range results {
		if r.err == nil {
			continue
		}
		failures++
		message := r.err.Error()
		kind := errorKind(message)
		byKind[kind]++
		if _, seen := sample[kind]; !seen {
			sample[kind] = message
		}
	}
	if failures == 0 {
		return
	}

	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	// Name order breaks ties so a run prints the same block every time.
	sort.Slice(kinds, func(i, j int) bool {
		if byKind[kinds[i]] != byKind[kinds[j]] {
			return byKind[kinds[i]] > byKind[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})

	fmt.Printf("\nError samples: %d failures in %s\n", failures, plural(len(kinds), "kind"))
	for i, kind := range kinds {
		if i >= errorKindsPrinted {
			fmt.Printf("  … and %s more\n", plural(len(kinds)-errorKindsPrinted, "kind"))
			break
		}
		fmt.Printf("  [%dx] %s\n", byKind[kind], sample[kind])
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// errorKindsPrinted caps the list; the header line above it states the total.
const errorKindsPrinted = 5

// errorKindNoise matches the run's numbering and the long hex of credentials and identifiers.
var errorKindNoise = regexp.MustCompile(`[0-9a-f]{8,}|[0-9]+`)

// errorKind strips the machine from a failure so equal failures count as one kind.
func errorKind(message string) string {
	return errorKindNoise.ReplaceAllString(message, "#")
}

// defaultHostnamePrefix is the name cleanup selects machines by.
const defaultHostnamePrefix = "soak"

// planAgents cycles tenants round-robin so a run is reproducible; a per-run prefix keeps its
// machines distinguishable.
func planAgents(n, tenants int, prefix string) []tenantAgent {
	if prefix == "" {
		prefix = defaultHostnamePrefix
	}
	plan := make([]tenantAgent, n)
	for i := 0; i < n; i++ {
		tenant := i % tenants
		plan[i] = tenantAgent{
			tenantIndex: tenant,
			agentIndex:  i,
			hostname:    fmt.Sprintf("%s-t%d-a%d", prefix, tenant, i),
		}
	}
	return plan
}

func percentile(durations []time.Duration, pct int) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	idx := (pct * len(sorted)) / 100
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx].Round(time.Millisecond)
}
