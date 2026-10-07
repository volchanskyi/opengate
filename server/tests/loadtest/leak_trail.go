package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	profileKindGoroutine = "goroutine"
	profileKindHeap      = "heap"
)

var profileKinds = []string{profileKindGoroutine, profileKindHeap}

const profileDirName = "profiles"

// profileFetchTimeout is well above the exposition readers' budget because a heap profile of a
// busy server is a large page.
const profileFetchTimeout = 30 * time.Second

const maxGrowthRows = 10

const maxReportedFrames = 12

// StackGrowth is one stack that got heavier between the first and last readings.
type StackGrowth struct {
	Kind string `json:"kind"`
	Unit string `json:"unit"`
	Site string `json:"site"`

	First float64 `json:"first"`
	Last  float64 `json:"last"`
	Delta float64 `json:"delta"`

	// IntervalsGrown out of Intervals separates a leak, which grows in nearly every interval,
	// from a working set that grew once.
	IntervalsGrown int `json:"intervals_grown"`
	Intervals      int `json:"intervals"`

	Frames []string `json:"frames,omitempty"`
}

// LeakTrail is the run's account of what grew and where the readings are kept.
type LeakTrail struct {
	IntervalSeconds float64 `json:"interval_seconds"`
	Snapshots       int     `json:"snapshots"`
	// Unread counts profiles the target would not answer with, so a trail built from partial
	// readings says so.
	Unread int `json:"unread,omitempty"`
	// Directory is relative to the bundle.
	Directory string        `json:"directory"`
	Growth    []StackGrowth `json:"growth"`
}

// GrowthAcross reports stacks that got heavier across the readings of one profile, heaviest
// first; a stack that shrank earns no credit against one that grew.
func GrowthAcross(kind, unit string, series [][]StackSample) []StackGrowth {
	if len(series) < 2 {
		return nil
	}

	weights := map[string][]float64{}
	stacks := map[string]StackSample{}
	for reading, samples := range series {
		for _, sample := range samples {
			key := sample.key()
			if _, seen := weights[key]; !seen {
				weights[key] = make([]float64, len(series))
				stacks[key] = sample
			}
			weights[key][reading] += sample.Weight
		}
	}

	var growth []StackGrowth
	for key, weighed := range weights {
		first, last := weighed[0], weighed[len(weighed)-1]
		if last <= first {
			continue
		}
		grown := 0
		for i := 1; i < len(weighed); i++ {
			if weighed[i] > weighed[i-1] {
				grown++
			}
		}
		growth = append(growth, StackGrowth{
			Kind:           kind,
			Unit:           unit,
			Site:           stacks[key].Site(),
			First:          first,
			Last:           last,
			Delta:          last - first,
			IntervalsGrown: grown,
			Intervals:      len(weighed) - 1,
			Frames:         reportedFrames(stacks[key]),
		})
	}

	// The site breaks a delta tie so runs of the same shape list the same order.
	sort.Slice(growth, func(i, j int) bool {
		if growth[i].Delta != growth[j].Delta {
			return growth[i].Delta > growth[j].Delta
		}
		return growth[i].Site < growth[j].Site
	})
	if len(growth) > maxGrowthRows {
		growth = growth[:maxGrowthRows]
	}
	return growth
}

func reportedFrames(sample StackSample) []string {
	frames := make([]string, 0, len(sample.Frames))
	for _, frame := range sample.Frames {
		frames = append(frames, frame.String())
		if len(frames) == maxReportedFrames {
			break
		}
	}
	return frames
}

type leakWatch struct {
	fetch func(kind string) (string, error)
	dir   string
	every time.Duration

	mu        sync.Mutex
	series    map[string][][]StackSample
	snapshots int
	unread    int

	done     chan struct{}
	finished chan struct{}
}

// start takes the first reading before the ticker so the fleet's arrival is not counted as growth.
func (w *leakWatch) start() func() *LeakTrail {
	w.series = map[string][][]StackSample{}
	w.done = make(chan struct{})
	w.finished = make(chan struct{})

	w.take()

	go func() {
		defer close(w.finished)
		ticker := time.NewTicker(w.every)
		defer ticker.Stop()
		for {
			select {
			case <-w.done:
				return
			case <-ticker.C:
				w.take()
			}
		}
	}()

	return func() *LeakTrail {
		close(w.done)
		<-w.finished
		// The closing reading follows the ticker's stop, so the series ends as the run left the target.
		w.take()
		return w.trail()
	}
}

func (w *leakWatch) take() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.snapshots++
	at := time.Now().UTC()
	for _, kind := range profileKinds {
		page, err := w.fetch(kind)
		if err != nil {
			fmt.Printf("::warning::could not take the target's %s profile: %v\n", kind, err)
			w.unread++
			continue
		}
		w.keep(kind, w.snapshots, at, page)

		stacks, err := parseProfile(kind, page)
		if err != nil {
			fmt.Printf("::warning::the target's %s profile could not be read: %v\n", kind, err)
			w.unread++
			continue
		}
		w.series[kind] = append(w.series[kind], stacks)
	}
}

func parseProfile(kind, page string) ([]StackSample, error) {
	switch kind {
	case profileKindGoroutine:
		return ParseGoroutineStacks(page)
	case profileKindHeap:
		return ParseHeapStacks(page)
	default:
		return nil, fmt.Errorf("no reader for a %q profile", kind)
	}
}

// keep writes each profile as it arrives, so a run killed at its job ceiling leaves what it took.
func (w *leakWatch) keep(kind string, seq int, at time.Time, page string) {
	dir := filepath.Join(w.dir, profileDirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		fmt.Printf("::warning::could not keep the target's profiles: %v\n", err)
		return
	}
	name := fmt.Sprintf("%s-%04d-%s.txt", kind, seq, at.Format("20060102T150405Z"))
	if err := os.WriteFile(filepath.Join(dir, name), []byte(page), 0o600); err != nil {
		fmt.Printf("::warning::could not keep %s: %v\n", name, err)
	}
}

func (w *leakWatch) trail() *LeakTrail {
	w.mu.Lock()
	defer w.mu.Unlock()

	trail := &LeakTrail{
		IntervalSeconds: w.every.Seconds(),
		Snapshots:       w.snapshots,
		Unread:          w.unread,
		Directory:       profileDirName,
	}
	for _, kind := range profileKinds {
		trail.Growth = append(trail.Growth, GrowthAcross(kind, weightUnit(kind), w.series[kind])...)
	}
	return trail
}

func weightUnit(kind string) string {
	if kind == profileKindHeap {
		return "bytes"
	}
	return "goroutines"
}

// WatchForLeaks returns a stop function that reports nil when the interval, target or bundle is
// missing.
func WatchForLeaks(metricsURL, bundleDir string, every time.Duration) func() *LeakTrail {
	if metricsURL == "" || bundleDir == "" || every <= 0 {
		return func() *LeakTrail { return nil }
	}
	watch := &leakWatch{
		fetch: func(kind string) (string, error) { return FetchProfilePage(metricsURL, kind) },
		dir:   bundleDir,
		every: every,
	}
	return watch.start()
}

// FetchProfilePage reads a profile as the symbolised debug=1 text from the target's cluster-only
// listener.
func FetchProfilePage(baseURL, kind string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), profileFetchTimeout)
	defer cancel()

	url := strings.TrimSuffix(baseURL, "/") + "/debug/pprof/" + kind + "?debug=1"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build %s profile request: %w", kind, err)
	}

	response, err := (&http.Client{Timeout: profileFetchTimeout}).Do(request)
	if err != nil {
		return "", fmt.Errorf("read %s profile: %w", kind, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("read %s profile: server answered %d", kind, response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read %s profile: %w", kind, err)
	}
	return string(body), nil
}

func printLeakTrail(trail *LeakTrail) {
	if trail == nil {
		return
	}
	fmt.Printf("\n=== What grew ===\n")
	fmt.Printf("Readings:    %d, %.0fs apart, kept in %s/\n",
		trail.Snapshots, trail.IntervalSeconds, trail.Directory)
	if trail.Unread > 0 {
		fmt.Printf("Unread:      %d profile(s) the target would not answer with\n", trail.Unread)
	}
	if len(trail.Growth) == 0 {
		fmt.Printf("Nothing the profiler can see grew across those readings.\n")
		return
	}
	for _, row := range trail.Growth {
		fmt.Printf("%-9s +%.0f %s (%.0f → %.0f), growing in %d of %d intervals\n  at %s\n",
			row.Kind, row.Delta, row.Unit, row.First, row.Last,
			row.IntervalsGrown, row.Intervals, row.Site)
	}
}
