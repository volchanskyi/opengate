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
	flag.Parse()

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

	type result struct {
		connectDur   time.Duration
		handshakeDur time.Duration
		registerDur  time.Duration
		err          error
	}

	results := make([]agentResult, *agents)
	var wg sync.WaitGroup

	for i := 0; i < *agents; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx] = runAgent(cm, *addr)
		}(i)
	}

	wg.Wait()
	totalDur := time.Since(start)

	// Collect statistics
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
	fmt.Printf("Agents:      %d/%d succeeded\n", successes, *agents)
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
		os.Exit(1)
	}
}

func runAgent(cm *cert.Manager, addr string) agentResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deviceID := uuid.New()

	tlsCert, err := cm.SignAgent(deviceID.String(), "loadtest")
	if err != nil {
		return agentResult{err: fmt.Errorf("sign cert: %w", err)}
	}

	agentTLS := cm.AgentTLSConfig(tlsCert)

	// Connect
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, addr, agentTLS, &quic.Config{
		MaxIdleTimeout: 30 * time.Second,
	})
	if err != nil {
		return agentResult{err: fmt.Errorf("dial: %w", err)}
	}
	connectDur := time.Since(t0)
	defer conn.CloseWithError(0, "loadtest done")

	// Accept control stream
	stream, err := conn.AcceptStream(ctx)
	if err != nil {
		return agentResult{connectDur: connectDur, err: fmt.Errorf("accept stream: %w", err)}
	}

	// Handshake
	t1 := time.Now()
	serverHello := make([]byte, 81)
	if _, err := io.ReadFull(stream, serverHello); err != nil {
		return agentResult{connectDur: connectDur, err: fmt.Errorf("read server hello: %w", err)}
	}

	agentCertDER := tlsCert.Certificate[0]
	agentCertHash := sha512.Sum384(agentCertDER)
	var nonce [32]byte
	copy(nonce[:], serverHello[1:33])
	agentHello := protocol.EncodeAgentHello(nonce, agentCertHash)
	if _, err := stream.Write(agentHello); err != nil {
		return agentResult{connectDur: connectDur, err: fmt.Errorf("write agent hello: %w", err)}
	}
	handshakeDur := time.Since(t1)

	// Register
	t2 := time.Now()
	codec := &protocol.Codec{}
	regMsg := &protocol.ControlMessage{
		Type:         protocol.MsgAgentRegister,
		Capabilities: []protocol.AgentCapability{protocol.CapTerminal},
		Hostname:     "loadtest-" + deviceID.String()[:8],
		OS:           "linux",
	}
	payload, err := codec.EncodeControl(regMsg)
	if err != nil {
		return agentResult{connectDur: connectDur, handshakeDur: handshakeDur, err: fmt.Errorf("encode register: %w", err)}
	}
	if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		return agentResult{connectDur: connectDur, handshakeDur: handshakeDur, err: fmt.Errorf("write register: %w", err)}
	}
	registerDur := time.Since(t2)

	return agentResult{
		connectDur:   connectDur,
		handshakeDur: handshakeDur,
		registerDur:  registerDur,
	}
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
