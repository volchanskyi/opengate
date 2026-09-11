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

// What grew inside the target, and where.
//
// The bundle already says whether a completed operation gave back what it took:
// the conservation reading brackets the run and divides what was not returned by
// the work between the two readings. That answers *whether*. It does not answer
// *where*, and the difference between those two answers is the difference
// between a finding and an instruction to go and reproduce a five-hour run.
//
// So the soak keeps profiles. Two of them, on an interval, for the whole run:
// the goroutine profile, which for a stuck-goroutine leak is the entire answer —
// it carries a count and a stack, so the defect that stranded two goroutines per
// finished session would have arrived as one line naming the file, the line
// number and the seven thousand goroutines sitting on it — and the heap profile,
// which says where a held object was born.
//
// Three properties are deliberate.
//
// *Every profile is kept.* The finding is the difference between two of them,
// and a difference cannot be recovered from a summary of either. Sixty-one
// snapshots of both profiles is about fifteen megabytes, which is an artifact
// rather than a problem.
//
// *They are kept as the text the target symbolised, not as the protocol buffer.*
// A pprof protocol buffer names addresses, and the tool that turns those into
// lines needs the exact binary that produced them. This family runs on a machine
// the job creates and destroys, so that binary does not outlive the run — a
// profile nobody can symbolise names no line at all. The `debug=1` text form is
// symbolised by the target itself, on the way out.
//
// *A profile that could not be taken is counted, not skipped.* A page that is
// not the profile it was asked for parses as no stacks, and no stacks reads as
// nothing grew, which is the healthiest answer there is. Every fetch is either
// a reading or an absence the trail carries.

// The two profiles a soak keeps, named as the target publishes them.
const (
	profileKindGoroutine = "goroutine"
	profileKindHeap      = "heap"
)

// profileKinds is the set taken on every round, in the order a reader meets
// them: the one that answers a leak outright first.
var profileKinds = []string{profileKindGoroutine, profileKindHeap}

// profileDirName is the folder inside the bundle the snapshots are kept in.
const profileDirName = "profiles"

// profileFetchTimeout bounds one profile fetch. A heap profile of a busy server
// is a large page written while the run is still driving load, so the budget is
// well above what the exposition readers beside it allow.
const profileFetchTimeout = 30 * time.Second

// maxGrowthRows is how many growing stacks each profile contributes to the
// bundle. The whole detail is in the kept files; this is the part somebody reads
// without opening them.
const maxGrowthRows = 10

// maxReportedFrames bounds how much of a stack travels with a growth row.
const maxReportedFrames = 12

// StackGrowth is one stack that got heavier between the first reading and the
// last, and how much of the run it spent getting heavier.
type StackGrowth struct {
	Kind string `json:"kind"`
	Unit string `json:"unit"`
	Site string `json:"site"`

	// First and Last are the readings the delta is taken between, so a reader
	// can tell a stack that doubled from two to four apart from one that went
	// from nought to seven thousand.
	First float64 `json:"first"`
	Last  float64 `json:"last"`
	Delta float64 `json:"delta"`

	// IntervalsGrown out of Intervals is what separates a leak from a working
	// set that got bigger once and then held. A leak grows in nearly every
	// interval; a server that filled a cache grows in one.
	IntervalsGrown int `json:"intervals_grown"`
	Intervals      int `json:"intervals"`

	Frames []string `json:"frames,omitempty"`
}

// LeakTrail is the run's account of what grew and where the readings are kept.
type LeakTrail struct {
	IntervalSeconds float64 `json:"interval_seconds"`
	Snapshots       int     `json:"snapshots"`
	// Unread is how many profiles the target would not answer with. It travels
	// because a trail assembled from half the readings it asked for says less
	// than one assembled from all of them, and nothing else in the file would
	// say so.
	Unread int `json:"unread,omitempty"`
	// Directory is where the kept profiles are, relative to the bundle.
	Directory string        `json:"directory"`
	Growth    []StackGrowth `json:"growth"`
}

// GrowthAcross reports what got heavier across a series of readings of one
// profile, heaviest growth first.
//
// A stack that shrank is not reported and is not a credit against one that
// grew: the question is what the target did not give back, and a target that
// gave back more of something else answers a different one.
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

	// Heaviest first, and the site breaks a tie so two runs of the same shape
	// produce the same order.
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

// leakWatch takes both profiles on an interval and keeps every one.
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

// start begins watching and returns what stops it and reports what it found.
//
// The first reading is taken before the ticker rather than after it, because a
// trail whose first reading is five minutes into the load has the arrival of
// the whole fleet inside it — every connection the run was about to make looks
// like growth that was always there.
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
		// The closing reading is taken after the ticker has stopped, so the
		// last thing in the series is the target as the run left it.
		w.take()
		return w.trail()
	}
}

// take reads both profiles once, keeps each on disk, and adds what it could
// parse to the series.
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

// parseProfile dispatches to the reader for the kind asked for, so an unknown
// kind is a refusal rather than an empty profile.
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

// keep writes one profile beside the bundle. A run killed at its job's ceiling
// still leaves everything it took up to that point, which is the reason the
// files are written as they arrive rather than at the end.
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

// trail is what the watch found.
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

// weightUnit names what a kind's numbers are counted in, so a row is readable
// without knowing which profile it came from.
func weightUnit(kind string) string {
	if kind == profileKindHeap {
		return "bytes"
	}
	return "goroutines"
}

// WatchForLeaks watches a live target, or watches nothing and says so.
//
// A run that was not asked for an interval, was given no target to read and no
// bundle to keep the readings in, reports no trail at all — which is different
// from a trail reporting that nothing grew.
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

// FetchProfilePage reads one of the target's profiles in its symbolised text
// form, off the cluster-only listener the exposition readers already use.
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

// printLeakTrail says what grew, for whoever is reading the log rather than the
// bundle.
//
// A soak's whole subject is a slope, and a slope that only ever appears inside
// an artifact is a finding nobody meets until they go looking for one.
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
