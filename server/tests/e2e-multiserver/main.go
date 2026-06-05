package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

// config holds the topology coordinates, resolved from the E2E_* environment the
// orchestration script (scripts/e2e-multiserver.sh) sets, with localhost defaults
// for ad-hoc runs against an already-up stack.
type config struct {
	serverAURL     string
	serverBURL     string
	databaseURL    string
	composeFile    string
	composeProject string
}

func loadConfig() config {
	return config{
		serverAURL:     envOr("E2E_SERVER_A_URL", "http://localhost:18081"),
		serverBURL:     envOr("E2E_SERVER_B_URL", "http://localhost:18082"),
		databaseURL:    envOr("E2E_DATABASE_URL", "postgres://opengate:e2e-test-password@localhost:15432/opengate?sslmode=disable"),
		composeFile:    envOr("E2E_COMPOSE_FILE", "deploy/docker-compose.multiserver.yml"),
		composeProject: envOr("E2E_COMPOSE_PROJECT", "opengate-ms"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// positiveInt parses v as a strictly-positive integer, returning ok=false for
// empty, malformed, or non-positive values.
func positiveInt(v string) (int, bool) {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func main() {
	// Gated so an accidental `go run ./tests/e2e-multiserver` (or a `go build`/vet
	// sweep) never tries to drive a stack that isn't there. `make e2e-multiserver`
	// sets this after bringing the topology up.
	if os.Getenv("OPENGATE_MULTISERVER_E2E") != "1" {
		log.Println("multiserver e2e is gated: set OPENGATE_MULTISERVER_E2E=1 and run via `make e2e-multiserver`. Skipping.")
		return
	}
	if err := run(); err != nil {
		log.Fatalf("multiserver e2e FAILED: %v", err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	logf := func(format string, args ...any) { log.Printf(format, args...) }
	h, err := newHarness(ctx, loadConfig(), logf)
	if err != nil {
		return err
	}
	defer h.close()

	// Load-baseline mode: when E2E_LOAD_SAMPLES is a positive int, measure
	// proxied-vs-direct relay latency instead of running the correctness scenarios.
	if samples, ok := positiveInt(os.Getenv("E2E_LOAD_SAMPLES")); ok {
		return loadBaseline(ctx, h, samples)
	}

	all := scenarios()
	var failures int
	for _, sc := range all {
		log.Printf("=== scenario: %s ===", sc.name)
		start := time.Now()
		if err := sc.run(ctx, h); err != nil {
			log.Printf("--- FAIL %s (%s): %v", sc.name, time.Since(start).Round(time.Millisecond), err)
			failures++
			continue
		}
		log.Printf("--- PASS %s (%s)", sc.name, time.Since(start).Round(time.Millisecond))
	}
	if failures > 0 {
		return fmt.Errorf("%d/%d scenarios failed", failures, len(all))
	}
	log.Printf("all %d multiserver scenarios passed", len(all))
	return nil
}
