// Package main implements a QUIC agent load harness that spawns N concurrent
// agent connections, performs the full mTLS handshake and registration, and
// reports timing statistics.
//
// Usage:
//
//	go run ./tests/loadtest/ -agents=100 -addr=127.0.0.1:9090 -data-dir=/tmp/loadtest
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha512"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/protocol"
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

	credentials, err := newAgentCredentials(dir, *enrollURL, *enrollToken)
	if err != nil {
		log.Fatalf("agent credentials: %v", err)
	}

	fmt.Printf("Starting QUIC load test: %d agents across %d tenant(s) → %s\n", *agents, tenants, *addr)
	start := time.Now()

	results := make([]agentResult, *agents)
	var wg sync.WaitGroup

	for i := 0; i < *agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runAgent(credentials, *addr, agentPlan[idx], opts)
		}(i)
	}

	wg.Wait()
	totalDur := time.Since(start)

	failures := reportResults(results, totalDur, *agents)

	if *bundleDir != "" {
		if err := writeRunBundle(*bundleDir, profile, results, start, totalDur, *agents, *addr); err != nil {
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

func runAgent(credentials agentCredentials, addr string, plan tenantAgent, opts loadOptions) agentResult {
	// The deadline covers connecting and registering, plus however long this
	// machine was asked to stay. A fixed budget would cut a held-open fleet
	// short and report the run's own timeout as the server dropping machines.
	ctx, cancel := context.WithTimeout(context.Background(), agentDeadline+opts.holdFor)
	defer cancel()

	tlsConfig, err := credentials.forAgent(ctx, plan)
	if err != nil {
		return agentResult{err: err}
	}

	// Connect.
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, addr, tlsConfig, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	if err != nil {
		return agentResult{err: fmt.Errorf("dial: %w", err)}
	}
	res := agentResult{connectDur: time.Since(t0)}
	defer conn.CloseWithError(0, "loadtest done")

	// Open control stream (agent-initiated): the agent opens and writes first,
	// per RFC 9000 stream-discovery.
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		res.err = fmt.Errorf("open stream: %w", err)
		return res
	}

	t1 := time.Now()
	if err := handshake(stream, tlsConfig.Certificates[0].Certificate[0]); err != nil {
		res.err = err
		return res
	}
	res.handshakeDur = time.Since(t1)

	codec := &protocol.Codec{}
	t2 := time.Now()
	if err := register(codec, stream, plan.hostname, agentCapabilities(opts)); err != nil {
		res.err = err
		return res
	}
	res.registerDur = time.Since(t2)

	if err := runSoakTraffic(codec, stream, opts); err != nil {
		res.err = err
	}
	return res
}

// handshake performs the agent-first mTLS control handshake: it sends AgentHello
// (nonce + cert hash) and reads the fixed-size ServerHello reply.
func handshake(stream io.ReadWriter, certDER []byte) error {
	certHash := sha512.Sum384(certDER)
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	if _, err := stream.Write(protocol.EncodeAgentHello(nonce, certHash)); err != nil {
		return fmt.Errorf("write agent hello: %w", err)
	}
	if _, err := io.ReadFull(stream, make([]byte, 81)); err != nil {
		return fmt.Errorf("read server hello: %w", err)
	}
	return nil
}

// agentCapabilities advertises the capabilities the soak exercises: Terminal
// always, plus Backfill when the reconnect-storm scenario is enabled (the
// server gates backfill admission on the advertised capability).
func agentCapabilities(opts loadOptions) []protocol.AgentCapability {
	caps := []protocol.AgentCapability{protocol.CapTerminal}
	if opts.backfillBatches > 0 {
		caps = append(caps, protocol.CapBackfill)
	}
	return caps
}

// register sends the AgentRegister control frame that completes enrollment.
func register(codec *protocol.Codec, w io.Writer, hostname string, caps []protocol.AgentCapability) error {
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: caps,
		Hostname:     hostname,
		OS:           "linux",
		Arch:         "amd64",
		Version:      "0.1.0",
	})
	if err != nil {
		return fmt.Errorf("encode register: %w", err)
	}
	if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write register: %w", err)
	}
	return nil
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
