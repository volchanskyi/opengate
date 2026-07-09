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

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

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
	logWindows := flag.Int("log-windows", 0, "log-rate metric windows each agent emits after register")
	answerLogPulls := flag.Bool("answer-log-pulls", false, "answer one on-demand raw-log pull per agent")
	flag.Parse()

	opts := loadOptions{logWindows: *logWindows, answerLogPulls: *answerLogPulls}

	dir := *dataDir
	if dir == "" {
		var err error
		dir, err = os.MkdirTemp("", "loadtest-certs-*")
		if err != nil {
			log.Fatalf("create temp dir: %v", err)
		}
		defer os.RemoveAll(dir)
	}

	cm, err := cert.NewManager(dir)
	if err != nil {
		log.Fatalf("cert manager: %v", err)
	}

	fmt.Printf("Starting QUIC load test: %d agents → %s\n", *agents, *addr)
	start := time.Now()

	results := make([]agentResult, *agents)
	var wg sync.WaitGroup

	for i := 0; i < *agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runAgent(cm, *addr, opts)
		}(i)
	}

	wg.Wait()
	totalDur := time.Since(start)

	if reportResults(results, totalDur, *agents) > 0 {
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

func runAgent(cm *cert.Manager, addr string, opts loadOptions) agentResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceID := uuid.New()

	tlsCert, err := cm.SignAgent(deviceID.String(), "loadtest")
	if err != nil {
		return agentResult{err: fmt.Errorf("sign cert: %w", err)}
	}

	// Connect.
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, addr, cm.AgentTLSConfig(tlsCert), &quic.Config{
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
	if err := handshake(stream, tlsCert.Certificate[0]); err != nil {
		res.err = err
		return res
	}
	res.handshakeDur = time.Since(t1)

	codec := &protocol.Codec{}
	t2 := time.Now()
	if err := register(codec, stream, deviceID); err != nil {
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

// register sends the AgentRegister control frame that completes enrollment.
func register(codec *protocol.Codec, w io.Writer, deviceID uuid.UUID) error {
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
		Hostname:     "loadtest-" + deviceID.String()[:8],
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
