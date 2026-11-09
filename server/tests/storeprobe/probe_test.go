package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestACatchUpRequestFillsTheBudgetToWithinOneReading(t *testing.T) {
	source := newCatchUpSource(newEncoder(), 50, time.Unix(1_700_000_000, 0))

	for range 5 {
		body, readings, err := source.next(requestBytes)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(body), requestBytes, "a request never passes its byte budget")
		longest := 0
		for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
			longest = max(longest, len(line)+1)
		}
		assert.Greater(t, len(body)+longest, requestBytes, "one more reading would not have fit")
		assert.Equal(t, readings, strings.Count(string(body), "\n"), "each reading is one line")
	}
}

func TestCatchUpReadingsAreTheServersOwnLinesAndNeverRepeat(t *testing.T) {
	source := newCatchUpSource(newEncoder(), 3, time.Unix(1_700_000_000, 0))
	seen := map[string]bool{}
	for range 20 {
		body, _, err := source.next(8 * 1024)
		require.NoError(t, err)
		for _, line := range strings.Split(strings.TrimSuffix(string(body), "\n"), "\n") {
			assert.True(t, strings.HasPrefix(line, "opengate_edge_metric_avg{"), "the server's catch-up series")
			assert.Contains(t, line, `device_id="`)
			assert.Contains(t, line, `tenant_id="`)
			fields := strings.Fields(line)
			reading := fields[0] + " at " + fields[len(fields)-1]
			assert.False(t, seen[reading], "a reading is sent once: %s", reading)
			seen[reading] = true
		}
	}
}

func TestALiveSecondCarriesTheReadingsItIsAskedFor(t *testing.T) {
	body, readings, err := liveSecond(newEncoder(), 2000, time.Unix(1_700_000_000, 0))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, readings, 2000)
	assert.Less(t, readings, 2000+len(vitals), "whole machines, the last one completing the count")
	assert.Equal(t, readings, bytes.Count(body, []byte("\n")))
}

// countingStore is a store that accepts every import after holding it for delay, and counts lines.
type countingStore struct {
	delay   time.Duration
	lines   atomic.Int64
	refuse  atomic.Bool
	metrics string
	// sequence, when set, is served one reading a call, the last one repeated.
	sequence []string
	served   int
	mu       sync.Mutex
}

func (s *countingStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v1/import/prometheus":
		body, _ := io.ReadAll(r.Body)
		time.Sleep(s.delay)
		if s.refuse.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		s.lines.Add(int64(bytes.Count(body, []byte("\n"))))
		w.WriteHeader(http.StatusNoContent)
	case "/metrics":
		s.mu.Lock()
		defer s.mu.Unlock()
		if len(s.sequence) > 0 {
			_, _ = io.WriteString(w, s.sequence[min(s.served, len(s.sequence)-1)])
			s.served++
			return
		}
		_, _ = io.WriteString(w, s.metrics)
	default:
		http.NotFound(w, r)
	}
}

func TestAProbeCountsWhatTheStoreAcceptedAndTimesTheLiveWriter(t *testing.T) {
	// A store this slow holds a catch-up request open when the probe's time runs out.
	store := &countingStore{delay: 40 * time.Millisecond,
		metrics: "process_resident_memory_bytes 1.5e+08\nprocess_start_time_seconds 1.7e+09\n"}
	server := httptest.NewServer(store)
	defer server.Close()

	reading, err := runProbe(context.Background(), probeConfig{
		store: server.URL, senders: 2, duration: 600 * time.Millisecond, requestBytes: 16 * 1024,
		liveRate: 200, liveEvery: 100 * time.Millisecond, devices: 20, memoryLimit: 512 << 20,
	})
	require.NoError(t, err)

	assert.Equal(t, 2, reading.Senders)
	assert.Equal(t, store.lines.Load(), reading.CatchUp.Readings+reading.Live.Readings,
		"every line the store took is counted once, by the writer that sent it")
	assert.InDelta(t, float64(reading.CatchUp.Readings)/reading.DurationSeconds, reading.CatchUp.ReadingsPerSecond, 0.001)
	assert.GreaterOrEqual(t, reading.Live.Requests, 5)
	assert.Equal(t, reading.Live.Requests, reading.Live.WithinFiveSeconds)
	assert.InDelta(t, 1.5e8, reading.Store.PeakMemoryBytes, 0)
	assert.False(t, reading.Store.Restarted)
}

func TestARefusedImportIsCountedAsFailedNeverAsStored(t *testing.T) {
	store := &countingStore{metrics: "process_resident_memory_bytes 1\nprocess_start_time_seconds 1\n"}
	store.refuse.Store(true)
	server := httptest.NewServer(store)
	defer server.Close()

	reading, err := runProbe(context.Background(), probeConfig{
		store: server.URL, senders: 1, duration: 300 * time.Millisecond, requestBytes: 8 * 1024,
		liveRate: 100, liveEvery: 100 * time.Millisecond, devices: 5, memoryLimit: 1 << 30,
	})
	require.NoError(t, err)
	assert.Zero(t, reading.CatchUp.Readings)
	assert.Positive(t, reading.CatchUp.Failed)
	assert.Zero(t, reading.Live.WithinFiveSeconds, "a refused live request is a miss")
}

func TestAStoreThatRestartedDuringTheProbeIsSaid(t *testing.T) {
	store := &countingStore{metrics: "process_resident_memory_bytes 1\nprocess_start_time_seconds 1\n"}
	server := httptest.NewServer(store)
	defer server.Close()
	cfg := probeConfig{store: server.URL, memoryLimit: 1 << 30}

	first, err := readStore(context.Background(), cfg)
	require.NoError(t, err)
	store.mu.Lock()
	store.metrics = "process_resident_memory_bytes 2\nprocess_start_time_seconds 9\n"
	store.mu.Unlock()
	second, err := readStore(context.Background(), cfg)
	require.NoError(t, err)
	assert.NotEqual(t, first.started, second.started)
}

func TestAQuietMinuteReadsTheStoresShareOfItsLimits(t *testing.T) {
	store := &countingStore{sequence: []string{
		"process_cpu_seconds_total 10\nprocess_resident_memory_bytes 1.0e+08\nprocess_start_time_seconds 5\n",
		"process_cpu_seconds_total 11\nprocess_resident_memory_bytes 2.56e+08\nprocess_start_time_seconds 5\n",
		"process_cpu_seconds_total 10.05\nprocess_resident_memory_bytes 1.2e+08\nprocess_start_time_seconds 5\n",
	}}
	server := httptest.NewServer(store)
	defer server.Close()

	quiet, err := readQuiet(context.Background(), probeConfig{
		store: server.URL, quiet: 100 * time.Millisecond, cpuLimit: 0.5, memoryLimit: 512 << 20,
	})
	require.NoError(t, err)
	assert.Greater(t, quiet.CPUShare, 0.0, "cores used over the minute against the store's allowance")
	assert.InDelta(t, 2.56e8/float64(512<<20), quiet.MemoryShare, 0.001, "the largest reading against the limit")
}

func TestTheProbesReadingIsWrittenAsJSON(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, writeJSON(&out, ProbeReading{Senders: 1, DurationSeconds: 900}))
	var back map[string]any
	require.NoError(t, json.Unmarshal(out.Bytes(), &back))
	assert.InDelta(t, 900, back["duration_seconds"], 0)
	assert.Contains(t, fmt.Sprint(back), "catch_up")
}
