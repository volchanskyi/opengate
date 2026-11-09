package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// liveWatchMetric is the series a watched minute is looked for in, by its device and minute.
const liveWatchMetric = `opengate_edge_metric_avg{dim="cpu.total",device_id=~"%s"}`

// liveWatchPromise is the delay a live reading is promised within.
const liveWatchPromise = 5 * time.Second

// LiveDelay is how long the watched machines' live minutes took from leaving the machine to being
// readable in the store. WithinFiveSeconds counts the minutes seen inside the promise.
type LiveDelay struct {
	Sent              int     `json:"sent"`
	Seen              int     `json:"seen"`
	WithinFiveSeconds int     `json:"within_five_seconds"`
	P50Ms             float64 `json:"p50_ms"`
	P99Ms             float64 `json:"p99_ms"`
	MaxMs             float64 `json:"max_ms"`
}

type liveKey struct {
	device string
	ts     int64
}

// liveWatch reads the store's export each second for the minutes watched machines sent.
type liveWatch struct {
	store  string
	client *http.Client

	mu      sync.Mutex
	pending map[liveKey]time.Time
	delays  []time.Duration
	sent    int
}

func newLiveWatch(store string, client *http.Client) *liveWatch {
	return &liveWatch{store: strings.TrimRight(store, "/"), client: client, pending: map[liveKey]time.Time{}}
}

// sentAt records that device sent the minute stamped ts at the moment at; a nil watch records
// nothing.
func (w *liveWatch) sentAt(device string, ts int64, at time.Time) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending[liveKey{device, ts}] = at
	w.sent++
}

// run polls each second until ctx ends; a refused read is retried on the next tick.
func (w *liveWatch) run(ctx context.Context) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			_ = w.poll(ctx, now)
		}
	}
}

// poll reads every pending minute's series once and times each minute it finds.
func (w *liveWatch) poll(ctx context.Context, now time.Time) error {
	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return nil
	}
	devices := map[string]bool{}
	oldest := int64(0)
	for key := range w.pending {
		devices[key.device] = true
		if oldest == 0 || key.ts < oldest {
			oldest = key.ts
		}
	}
	w.mu.Unlock()

	names := make([]string, 0, len(devices))
	for device := range devices {
		names = append(names, regexp.QuoteMeta(device))
	}
	sort.Strings(names)
	query := url.Values{
		"match[]": {fmt.Sprintf(liveWatchMetric, strings.Join(names, "|"))},
		"start":   {fmt.Sprint(oldest)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, w.store+"/api/v1/export?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("store export answered %d", resp.StatusCode)
	}
	return w.found(bufio.NewScanner(resp.Body), now)
}

// found times each pending minute the export lines hold.
func (w *liveWatch) found(lines *bufio.Scanner, now time.Time) error {
	lines.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	w.mu.Lock()
	defer w.mu.Unlock()
	for lines.Scan() {
		var series struct {
			Metric     map[string]string `json:"metric"`
			Timestamps []int64           `json:"timestamps"`
		}
		if err := json.Unmarshal(lines.Bytes(), &series); err != nil {
			return fmt.Errorf("store export line: %w", err)
		}
		for _, millis := range series.Timestamps {
			key := liveKey{series.Metric["device_id"], millis / 1000}
			if at, ok := w.pending[key]; ok {
				w.delays = append(w.delays, now.Sub(at))
				delete(w.pending, key)
			}
		}
	}
	return lines.Err()
}

// result is the watch's delay figures, or nil for a watch nothing was sent to.
func (w *liveWatch) result() *LiveDelay {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sent == 0 {
		return nil
	}
	delays := slices.Clone(w.delays)
	slices.Sort(delays)
	out := &LiveDelay{Sent: w.sent, Seen: len(delays)}
	for _, d := range delays {
		if d <= liveWatchPromise {
			out.WithinFiveSeconds++
		}
	}
	if len(delays) > 0 {
		out.P50Ms = millis(delays[len(delays)/2])
		out.P99Ms = millis(delays[min(len(delays)-1, len(delays)*99/100)])
		out.MaxMs = millis(delays[len(delays)-1])
	}
	return out
}

// startLiveWatch starts a watch on store for the first machines of the run and returns the
// function that stops it and reads its figures; an empty store watches nothing.
func startLiveWatch(opts *loadOptions, store string, machines int) func() *LiveDelay {
	if store == "" || machines <= 0 {
		return func() *LiveDelay { return nil }
	}
	opts.liveWatch = newLiveWatch(store, &http.Client{Timeout: 5 * time.Second})
	opts.watchMachines = machines
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		opts.liveWatch.run(ctx)
	}()
	return func() *LiveDelay {
		cancel()
		<-done
		return opts.liveWatch.result()
	}
}

// writeLiveDelay prints the watched machines' live delay; nil prints nothing.
func writeLiveDelay(w io.Writer, d *LiveDelay) {
	if d == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "Live delay:  %d of %d watched minutes seen, %d within 5s; p50 %.0f ms, p99 %.0f ms, max %.0f ms\n",
		d.Seen, d.Sent, d.WithinFiveSeconds, d.P50Ms, d.P99Ms, d.MaxMs)
}
