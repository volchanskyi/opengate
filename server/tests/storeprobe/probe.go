package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// requestBytes is the shared writer's flush size, the catch-up request the probe sends.
const requestBytes = 256 * 1024

// livePromise is the delay a live reading is promised within.
const livePromise = 5 * time.Second

// probeConfig is one probe run against the store at store.
type probeConfig struct {
	store        string
	senders      int
	duration     time.Duration
	requestBytes int
	liveRate     int
	liveEvery    time.Duration
	devices      int
	quiet        time.Duration
	cpuLimit     float64
	memoryLimit  float64
}

// CatchUpReading is what the catch-up senders put into the store.
type CatchUpReading struct {
	Requests          int64   `json:"requests"`
	Readings          int64   `json:"readings"`
	Failed            int64   `json:"failed"`
	ReadingsPerSecond float64 `json:"readings_per_second"`
}

// LiveReading is the live-shaped writer's requests and how many the store took within the promise.
type LiveReading struct {
	Requests          int     `json:"requests"`
	Readings          int64   `json:"readings"`
	WithinFiveSeconds int     `json:"within_five_seconds"`
	P99Ms             float64 `json:"p99_ms"`
}

// StoreReading is the store's own account of the run: its largest resident memory against its
// limit, and whether it started again.
type StoreReading struct {
	PeakMemoryBytes  float64 `json:"peak_memory_bytes"`
	MemoryLimitBytes float64 `json:"memory_limit_bytes"`
	Restarted        bool    `json:"restarted"`
}

// ProbeReading is one probe run.
type ProbeReading struct {
	Senders         int            `json:"senders"`
	DurationSeconds float64        `json:"duration_seconds"`
	RequestBytes    int            `json:"request_bytes"`
	CatchUp         CatchUpReading `json:"catch_up"`
	Live            LiveReading    `json:"live"`
	Store           StoreReading   `json:"store"`
}

// QuietReading is the store's share of its limits over a minute of background load alone.
type QuietReading struct {
	Seconds     float64 `json:"seconds"`
	CPUShare    float64 `json:"cpu_share"`
	MemoryShare float64 `json:"memory_share"`
}

// storeState is one read of the store's own process figures.
type storeState struct {
	cpuSeconds, residentBytes, started float64
}

// readStore reads the store's process figures from its own metrics page.
func readStore(ctx context.Context, cfg probeConfig) (storeState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.store+"/metrics", nil)
	if err != nil {
		return storeState{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return storeState{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return storeState{}, fmt.Errorf("store metrics answered %d", resp.StatusCode)
	}
	var state storeState
	lines := bufio.NewScanner(resp.Body)
	lines.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for lines.Scan() {
		name, value, ok := strings.Cut(lines.Text(), " ")
		if !ok {
			continue
		}
		into := map[string]*float64{
			"process_cpu_seconds_total":     &state.cpuSeconds,
			"process_resident_memory_bytes": &state.residentBytes,
			"process_start_time_seconds":    &state.started,
		}[name]
		if into == nil {
			continue
		}
		if *into, err = strconv.ParseFloat(strings.TrimSpace(value), 64); err != nil {
			return storeState{}, fmt.Errorf("store metric %s: %w", name, err)
		}
	}
	return state, lines.Err()
}

// readQuiet reads the store across cfg.quiet of background load alone, sampling its memory each
// tenth of the window.
func readQuiet(ctx context.Context, cfg probeConfig) (QuietReading, error) {
	first, err := readStore(ctx, cfg)
	if err != nil {
		return QuietReading{}, err
	}
	started := time.Now()
	peak, last := first.residentBytes, first
	for time.Since(started) < cfg.quiet {
		time.Sleep(cfg.quiet / 10)
		if last, err = readStore(ctx, cfg); err != nil {
			return QuietReading{}, err
		}
		peak = max(peak, last.residentBytes)
	}
	seconds := time.Since(started).Seconds()
	return QuietReading{
		Seconds:     seconds,
		CPUShare:    (last.cpuSeconds - first.cpuSeconds) / seconds / cfg.cpuLimit,
		MemoryShare: peak / cfg.memoryLimit,
	}, nil
}

// importer sends import bodies to the store and reports whether each was taken.
type importer struct {
	url    string
	client *http.Client
}

func (im importer) send(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, im.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; version=0.0.4")
	resp, err := im.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("store import answered %d", resp.StatusCode)
	}
	return nil
}

// runProbe writes catch-up from cfg.senders senders beside the live-shaped writer for
// cfg.duration, and reads the store's memory as it goes.
func runProbe(ctx context.Context, cfg probeConfig) (ProbeReading, error) {
	before, err := readStore(ctx, cfg)
	if err != nil {
		return ProbeReading{}, err
	}
	im := importer{url: cfg.store + "/api/v1/import/prometheus", client: &http.Client{Timeout: 30 * time.Second}}
	runCtx, cancel := context.WithTimeout(ctx, cfg.duration)
	defer cancel()

	var catchUp CatchUpReading
	var requests, readings, failed atomic.Int64
	var live liveTally
	var peak atomic.Uint64
	peak.Store(uint64(before.residentBytes))
	var wg sync.WaitGroup
	started := time.Now()
	for range cfg.senders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			source := newCatchUpSource(newEncoder(), cfg.devices, started)
			for runCtx.Err() == nil {
				body, n, err := source.next(cfg.requestBytes)
				if err != nil {
					failed.Add(1)
					return
				}
				requests.Add(1)
				// A request open when the time runs out is finished, so what the store took is counted.
				if err := im.send(context.WithoutCancel(runCtx), body); err != nil {
					failed.Add(1)
					continue
				}
				readings.Add(int64(n))
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		live.run(runCtx, im, cfg)
	}()
	go func() {
		defer wg.Done()
		watchMemory(runCtx, cfg, &peak)
	}()
	wg.Wait()
	elapsed := time.Since(started).Seconds()

	after, err := readStore(ctx, cfg)
	restarted := err != nil || after.started != before.started
	peak.Store(max(peak.Load(), uint64(after.residentBytes)))
	catchUp = CatchUpReading{Requests: requests.Load(), Readings: readings.Load(), Failed: failed.Load(),
		ReadingsPerSecond: float64(readings.Load()) / elapsed}
	return ProbeReading{
		Senders: cfg.senders, DurationSeconds: elapsed, RequestBytes: cfg.requestBytes,
		CatchUp: catchUp, Live: live.reading(),
		Store: StoreReading{PeakMemoryBytes: float64(peak.Load()), MemoryLimitBytes: cfg.memoryLimit, Restarted: restarted},
	}, nil
}

// watchMemory keeps the largest resident memory the store reports each five seconds.
func watchMemory(ctx context.Context, cfg probeConfig, peak *atomic.Uint64) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if state, err := readStore(ctx, cfg); err == nil && uint64(state.residentBytes) > peak.Load() {
				peak.Store(uint64(state.residentBytes))
			}
		}
	}
}

// liveTally times the live-shaped writer's requests; a refused one never counts as within.
type liveTally struct {
	mu       sync.Mutex
	took     []time.Duration
	missed   int
	readings int64
}

// run sends one live second each cfg.liveEvery until ctx ends.
func (l *liveTally) run(ctx context.Context, im importer, cfg probeConfig) {
	enc := newEncoder()
	tick := time.NewTicker(cfg.liveEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			body, n, err := liveSecond(enc, cfg.liveRate, now)
			if err != nil {
				return
			}
			sent := time.Now()
			err = im.send(context.WithoutCancel(ctx), body)
			l.mu.Lock()
			if err != nil {
				l.missed++
			} else {
				l.took = append(l.took, time.Since(sent))
				l.readings += int64(n)
			}
			l.mu.Unlock()
		}
	}
}

func (l *liveTally) reading() LiveReading {
	l.mu.Lock()
	defer l.mu.Unlock()
	took := slices.Clone(l.took)
	slices.Sort(took)
	out := LiveReading{Requests: len(took) + l.missed, Readings: l.readings}
	for _, d := range took {
		if d <= livePromise {
			out.WithinFiveSeconds++
		}
	}
	if len(took) > 0 {
		out.P99Ms = float64(took[min(len(took)-1, len(took)*99/100)]) / float64(time.Millisecond)
	}
	return out
}

// writeJSON writes one reading as indented JSON.
func writeJSON(w io.Writer, reading any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(reading)
}
