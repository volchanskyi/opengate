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

type agentResult struct {
	connectDur   time.Duration
	handshakeDur time.Duration
	registerDur  time.Duration
	err          error
}

func main() {
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

	failures := reportResults(results, totalDur, *agents)

	// Registration as the server measured it, where the device row lands. The
	// harness's own clock stops at a local send buffer, which cannot move
	// however slow the write becomes.
	registration := readServerRegistration(*metricsURL)

	if *bundleDir != "" {
		err := writeRunBundle(runBundleInputs{
			Profile:      profile,
			Results:      results,
			StartedAt:    start,
			Total:        totalDur,
			AgentCount:   *agents,
			Target:       *addr,
			Phases:       phases,
			Registration: registration,
			Fixture:      fixture,
		}, *bundleDir)
		if err != nil {
			log.Fatalf("bundle: %v", err)
		}
	}

	if failures > 0 {
		os.Exit(1)
	}
}

// reportResults prints the timing summary and returns the number of failed
// agents, so the caller can set the process exit code.
func reportResults(results []agentResult, totalDur time.Duration, agents int) int {
	var (
		successes    int
		failures     int
		connectTimes []time.Duration
		hsTimes      []time.Duration
		regTimes     []time.Duration
	)
	for _, r := range results {
		if r.err != nil {
			failures++
			continue
		}
		successes++
		connectTimes = append(connectTimes, r.connectDur)
		hsTimes = append(hsTimes, r.handshakeDur)
		regTimes = append(regTimes, r.registerDur)
	}

	fmt.Printf("\n=== Results ===\n")
	fmt.Printf("Total time:  %s\n", totalDur.Round(time.Millisecond))
	fmt.Printf("Agents:      %d/%d succeeded\n", successes, agents)
	fmt.Printf("Failures:    %d\n", failures)

	if successes > 0 {
		fmt.Printf("\nConnect:     p50=%s  p95=%s  p99=%s\n",
			percentile(connectTimes, 50), percentile(connectTimes, 95), percentile(connectTimes, 99))
		fmt.Printf("Handshake:   p50=%s  p95=%s  p99=%s\n",
			percentile(hsTimes, 50), percentile(hsTimes, 95), percentile(hsTimes, 99))
		fmt.Printf("Register:    p50=%s  p95=%s  p99=%s\n",
			percentile(regTimes, 50), percentile(regTimes, 95), percentile(regTimes, 99))
	}

	if failures > 0 {
		printErrorSamples(results)
	}
	return failures
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
