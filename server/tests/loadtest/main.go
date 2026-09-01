// Package main implements a QUIC agent load harness that spawns N concurrent
// agent connections, performs the full mTLS handshake and registration, and
// reports timing statistics.
//
// Usage:
//
//	go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090 -data-dir=/tmp/loadtest
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"time"
)

// agentDeadline bounds connecting, handshaking and registering. Whatever hold
// the run asked for is added to it.
const agentDeadline = 30 * time.Second

// What the process returns, and what each code means to the runner reading it.
//
// A run has three outcomes and needs three codes. Deriving the code from the
// failure count alone collapses two of them: a run whose agents produced no
// result each, rather than a failed one each, counts zero failures and is
// indistinguishable from a clean run. The runner around this then keeps the
// output and the trend absorbs a night that measured nothing.
const (
	// exitAgentFailures is a run that measured the system and some of its
	// machines did not arrive. That is still a measurement — a fleet that half
	// connects is what the trend exists to record — so the runner keeps it.
	exitAgentFailures = 1
	// exitMeasuredNothing is a run that did not measure the system at all. Its
	// output describes the absence rather than the system, so the runner
	// discards it and the run is short a scenario.
	exitMeasuredNothing = 2
)

type agentResult struct {
	connectDur   time.Duration
	handshakeDur time.Duration
	registerDur  time.Duration

	// arrivedAt is when this machine finished registering — the moment it is
	// part of the fleet. It is what bounds the arrival window, and it is
	// deliberately not the moment the machine's life ended: a machine held open
	// for the generator beside it leaves when the run does, so an end time would
	// measure the hold.
	arrivedAt time.Time

	err error
}

// exitCode is what the process returns for a finished run.
//
// The verdict comes first and the failure count second, because they answer
// different questions: the count says how many machines did not arrive, and the
// verdict says whether what happened was a measurement at all. A run that
// measured nothing cannot be reported as one that did, however few of its
// machines failed.
func exitCode(verdict Verdict, failures int) int {
	switch {
	case verdict.Result == ResultInvalid:
		return exitMeasuredNothing
	case verdict.Result == ResultFailed, failures > 0:
		return exitAgentFailures
	default:
		return 0
	}
}

func main() {
	os.Exit(run())
}

// run is the whole harness, returning the code the process exits with. It is
// separated from main so the temp directory holding the certificates this run
// signed is removed on every path, including the ones that end badly.
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
	relaySessions := flag.Bool("relay-sessions", false, "answer SessionRequest by joining the machine side of the relay and echoing, so the browser side can time a real round trip")
	profilePath := flag.String("profile", "", "load/profiles/<name>.yaml declaring the phases, safety limits and gates")
	bundleDir := flag.String("bundle", "", "directory to write this run's evidence bundle into")
	enrollURL := flag.String("enroll-url", "", "server base URL to enroll each agent through, so no certificate authority key leaves the cluster")
	enrollToken := flag.String("enroll-token", "", "enrollment token to spend, minted through the admin API before the run")
	metricsURL := flag.String("metrics-url", "", "server base URL to read registration timing from, so the figure is the one the server measured where the device row landed rather than this process's own send buffer")
	fixtureAccount := flag.String("fixture-account", "", "administrator to build the fixture as; empty builds no fixture")
	fixturePasswordFlag := flag.String("fixture-password", "", "that administrator's password")
	fixtureSize := flag.String("fixture-size", "", "fleet to build before the run: small, large or lopsided; empty takes the profile's own")
	fixtureSeed := flag.Uint64("fixture-seed", 1, "the seed the fleet is derived from, so the same seed reproduces the same fleet")
	fixtureBootstrap := flag.Bool("fixture-bootstrap", false, "the environment starts empty, so register the administrator instead of signing in as one")
	flag.Parse()

	// Production is never a target, and the way a generator ends up pointed at
	// one is an address in an environment variable set in a hurry. The refusal
	// is here, before anything dials, rather than in a reviewer's attention.
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

	opts := loadOptions{
		defaultTelemetry:        *defaultTelemetry,
		telemetryCycles:         *telemetryCycles,
		metricWindows:           *metricWindows,
		answerLogPulls:          *answerLogPulls,
		backfillBatches:         *backfillBatches,
		backfillSamplesPerBatch: *backfillSamples,
		holdFor:                 *holdFor,
		relaySessions:           *relaySessions,
	}

	tenants := max(*tenantFlag, 1)
	agentPlan := planAgents(*agents, tenants)

	dir := *dataDir
	if dir == "" && *enrollURL == "" {
		var err error
		dir, err = os.MkdirTemp("", "loadtest-certs-*")
		if err != nil {
			log.Fatalf("create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
	}

	// A fleet is built before the clock starts. It is thousands of writes and
	// would be the largest thing in any phase it shared.
	fixture, spentToken := buildFixtureIfAsked(fixtureRequest{
		baseURL:   *enrollURL,
		account:   *fixtureAccount,
		password:  *fixturePasswordFlag,
		size:      *fixtureSize,
		seed:      *fixtureSeed,
		profile:   profile,
		bootstrap: *fixtureBootstrap,
	})
	if spentToken != "" {
		*enrollToken = spentToken
	}

	credentials, err := newAgentCredentials(dir, *enrollURL, *enrollToken)
	if err != nil {
		log.Fatalf("agent credentials: %v", err)
	}

	fmt.Printf("Starting QUIC load test: %d agents across %d tenant(s) → %s\n", *agents, tenants, *addr)
	start := time.Now()

	results, phases := runWorkload(profile, *agents, agentPlan, credentials, *addr, opts)
	totalDur := time.Since(start)

	// Registration as the server measured it, where the device row lands. The
	// harness's own clock stops at a local send buffer, which cannot move
	// however slow the write becomes — so the reading is taken before the
	// results block is printed, because the block is where it is published.
	registration := readServerRegistration(*metricsURL)

	failures := reportResults(results, start, totalDur, *agents, registration)

	// The bundle is built whether or not it is written, because it carries the
	// verdict — and the verdict is what says whether this run measured the
	// system. A run that reports its own outcome only into a file nobody reads
	// is the shape a green shard on a sweep that connected nobody came from.
	bundle := buildRunBundle(runBundleInputs{
		Profile:      profile,
		Results:      results,
		StartedAt:    start,
		Total:        totalDur,
		AgentCount:   *agents,
		Target:       *addr,
		Phases:       phases,
		Registration: registration,
		Fixture:      fixture,
	})

	if *bundleDir != "" {
		if err := writeRunBundle(bundle, *bundleDir); err != nil {
			log.Fatalf("bundle: %v", err)
		}
	}

	for _, reason := range bundle.Verdict.Reasons {
		fmt.Printf("::error::%s\n", reason)
	}
	return exitCode(bundle.Verdict, failures)
}

// arrivalWindow is how long the fleet took to arrive: from the run's start to
// the moment the last machine finished registering.
//
// It is the denominator of the run's arrival rate, and the run's own wall clock
// is not. A run keeps its fleet connected so the generator beside it has
// machines to open sessions against, so the clock is minutes of holding after a
// second of arriving — dividing by it reports the hold under the arrival's
// name, and a fleet that entirely arrived reads as one that never did.
//
// A machine that failed has no arrival, so it cannot be the last one. A fleet
// where nobody arrived has no window at all rather than a zero-length one: zero
// is the fastest run ever recorded, and this is the opposite of a run.
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

// reportResults prints the timing summary and returns the number of failed
// agents, so the caller can set the process exit code.
func reportResults(results []agentResult, start time.Time, totalDur time.Duration, agents int,
	registration *ServerRegistration,
) int {
	var (
		successes    int
		failures     int
		connectTimes []time.Duration
		hsTimes      []time.Duration
	)
	for _, r := range results {
		if r.err != nil {
			failures++
			continue
		}
		successes++
		connectTimes = append(connectTimes, r.connectDur)
		hsTimes = append(hsTimes, r.handshakeDur)
	}

	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Total time:  %s\n", totalDur.Round(time.Millisecond))
	fmt.Printf("Arrival window:  %s\n", arrivalWindow(results, start).Round(time.Millisecond))
	fmt.Printf("Agents:      %d/%d succeeded\n", successes, agents)
	fmt.Printf("Failures:    %d\n", failures)

	if successes > 0 {
		// Connect and handshake are the generator's own side of the wire, and it
		// is the only side that can see them.
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

// printRegisterLine publishes registration as the server measured it, where the
// device row lands.
//
// A run the server did not answer publishes no line at all. The harness has its
// own timing around the register frame, but that clock stops at a local send
// buffer: it reports microseconds whatever the write behind it costs, so it
// cannot say anything about registration, and two gate ceilings named after
// this line would go on sitting where they sat. An absent figure is honest.
func printRegisterLine(registration *ServerRegistration) {
	if registration == nil || !registration.Measured() {
		return
	}
	fmt.Printf("Register:    p50=%s  p95=%s  p99=%s\n",
		millisDuration(registration.QuantileMs(0.50)),
		millisDuration(registration.QuantileMs(0.95)),
		millisDuration(registration.QuantileMs(0.99)))
}

// millisDuration renders a millisecond figure in the same duration form the
// percentile lines beside it use, so one parser reads the whole block.
func millisDuration(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond)).Round(time.Microsecond)
}

// printErrorSamples prints up to three unique error messages from failed agents.
func printErrorSamples(results []agentResult) {
	seen := map[string]int{}
	for _, r := range results {
		if r.err != nil {
			seen[r.err.Error()]++
		}
	}
	fmt.Printf("\nError samples:\n")
	printed := 0
	for msg, cnt := range seen {
		fmt.Printf("  [%dx] %s\n", cnt, msg)
		printed++
		if printed >= 3 {
			break
		}
	}
}

// planAgents lays out n agents across tenants cohorts deterministically, so a
// soak run is reproducible: tenant index cycles round-robin and each hostname
// carries its tenant + agent index.
func planAgents(n, tenants int) []tenantAgent {
	plan := make([]tenantAgent, n)
	for i := 0; i < n; i++ {
		tenant := i % tenants
		plan[i] = tenantAgent{
			tenantIndex: tenant,
			agentIndex:  i,
			hostname:    fmt.Sprintf("soak-t%d-a%d", tenant, i),
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
