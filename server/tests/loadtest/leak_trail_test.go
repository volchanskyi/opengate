package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the soak has to be able to say, and what it must refuse to say.
//
// A five-hour run that reports "something grew" has told whoever reads it to go
// and do the work again. The readings below are what turn that into a line
// number, and every one of them is a reading rather than a literal: a profile
// the target would not answer is an absence, and an absence must never arrive
// as a trail of no growth — that is the shape that reports a leaking server
// clean.

// The finding is "this grew, at this line". Growth is stated against the stack
// it belongs to, and the count of intervals it grew in is what tells a leak
// apart from a working set that got bigger once and then held.
func TestGrowthAcrossNamesTheStackThatGrewAndHowOftenItGrew(t *testing.T) {
	first, err := ParseGoroutineStacks(goroutinePageAtStart)
	require.NoError(t, err)
	middlePage := strings.Replace(goroutinePageAtEnd, "28 @", "16 @", 1)
	middle, err := ParseGoroutineStacks(middlePage)
	require.NoError(t, err)
	last, err := ParseGoroutineStacks(goroutinePageAtEnd)
	require.NoError(t, err)

	growth := GrowthAcross("goroutine", "goroutines", [][]StackSample{first, middle, last})
	require.NotEmpty(t, growth)

	top := growth[0]
	assert.Equal(t, "goroutine", top.Kind)
	assert.Equal(t, "goroutines", top.Unit)
	assert.Contains(t, top.Site, "relay.(*Relay).pump")
	assert.Equal(t, float64(24), top.Delta)
	assert.Equal(t, float64(4), top.First)
	assert.Equal(t, float64(28), top.Last)
	assert.Equal(t, 2, top.IntervalsGrown)
	assert.Equal(t, 2, top.Intervals)
	assert.NotEmpty(t, top.Frames)
}

// A stack that did not grow is not a finding, and a stack that shrank is not a
// credit against one that did.
func TestGrowthAcrossReportsOnlyWhatGrew(t *testing.T) {
	first, err := ParseGoroutineStacks(goroutinePageAtEnd)
	require.NoError(t, err)
	last, err := ParseGoroutineStacks(goroutinePageAtStart)
	require.NoError(t, err)

	assert.Empty(t, GrowthAcross("goroutine", "goroutines", [][]StackSample{first, last}))
}

// A single reading has no difference in it. Reporting one as a trail would say
// nothing grew, which is the answer a five-hour leak also produces.
func TestGrowthAcrossNeedsTwoReadings(t *testing.T) {
	only, err := ParseGoroutineStacks(goroutinePageAtEnd)
	require.NoError(t, err)
	assert.Empty(t, GrowthAcross("goroutine", "goroutines", [][]StackSample{only}))
}

// A stack that appears only in the later reading grew by all of itself. It is
// the commonest shape of the defect: a path that was never taken before the
// load arrived.
func TestAStackThatAppearsLaterGrewByAllOfItself(t *testing.T) {
	first, err := ParseGoroutineStacks(`goroutine profile: total 1
1 @ 0x43e5ce
#	0x43e5cd	runtime.gopark+0x10d	/usr/local/go/src/runtime/proc.go:435
`)
	require.NoError(t, err)
	last, err := ParseGoroutineStacks(goroutinePageAtEnd)
	require.NoError(t, err)

	growth := GrowthAcross("goroutine", "goroutines", [][]StackSample{first, last})
	require.NotEmpty(t, growth)
	assert.Contains(t, growth[0].Site, "relay.(*Relay).pump")
	assert.Equal(t, float64(0), growth[0].First)
	assert.Equal(t, float64(28), growth[0].Last)
}

// fakeProfiles answers the two pages a watch asks for, and counts what it was
// asked so a test can prove the watch took the readings it reports.
type fakeProfiles struct {
	mu     sync.Mutex
	asked  map[string]int
	pages  map[string][]string
	broken map[string]bool
}

func newFakeProfiles() *fakeProfiles {
	return &fakeProfiles{
		asked: map[string]int{},
		pages: map[string][]string{
			profileKindGoroutine: {goroutinePageAtStart, goroutinePageAtEnd, goroutinePageAtEnd},
			profileKindHeap:      {heapPageAtStart, heapPageAtEnd, heapPageAtEnd},
		},
		broken: map[string]bool{},
	}
}

func (f *fakeProfiles) fetch(kind string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := min(f.asked[kind], len(f.pages[kind])-1)
	f.asked[kind]++
	if f.broken[kind] {
		return "", fmt.Errorf("the target would not answer for %s", kind)
	}
	return f.pages[kind][at], nil
}

func (f *fakeProfiles) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.asked[kind]
}

// Every profile is kept, because the difference between two of them is the
// finding and a difference cannot be recomputed from a summary. They are kept
// as the text the target symbolised, not as the protocol buffer: the machine
// that built the binary is destroyed with the run, and a profile that needs a
// binary nobody has any more names no line at all.
func TestWatchKeepsEveryProfileItTook(t *testing.T) {
	dir := t.TempDir()
	fake := newFakeProfiles()

	watch := &leakWatch{fetch: fake.fetch, dir: dir, every: time.Millisecond}
	stop := watch.start()
	require.Eventually(t, func() bool { return fake.count(profileKindGoroutine) >= 3 },
		2*time.Second, time.Millisecond)
	trail := stop()

	require.NotNil(t, trail)
	kept, err := filepath.Glob(filepath.Join(dir, profileDirName, "*.txt"))
	require.NoError(t, err)
	assert.Len(t, kept, trail.Snapshots*len(profileKinds))
	assert.GreaterOrEqual(t, trail.Snapshots, 3)

	body, err := os.ReadFile(kept[0])
	require.NoError(t, err)
	assert.Contains(t, string(body), "profile:")
}

func TestWatchReportsWhatGrewBetweenItsSnapshots(t *testing.T) {
	fake := newFakeProfiles()
	watch := &leakWatch{fetch: fake.fetch, dir: t.TempDir(), every: time.Millisecond}
	stop := watch.start()
	require.Eventually(t, func() bool { return fake.count(profileKindHeap) >= 2 },
		2*time.Second, time.Millisecond)
	trail := stop()

	require.NotNil(t, trail)
	sites := map[string]string{}
	for _, row := range trail.Growth {
		sites[row.Kind] = row.Site
	}
	assert.Contains(t, sites[profileKindGoroutine], "relay.(*Relay).pump")
	assert.Contains(t, sites[profileKindHeap], "store.(*Store).remember")
}

// A target that would not answer is an absence the trail carries, not a run
// that found nothing. A soak reporting a clean bill from a profiler it could
// never reach is the false green this whole reading exists to refuse.
func TestWatchCountsTheProfilesItCouldNotTake(t *testing.T) {
	fake := newFakeProfiles()
	fake.broken[profileKindHeap] = true

	watch := &leakWatch{fetch: fake.fetch, dir: t.TempDir(), every: time.Millisecond}
	stop := watch.start()
	require.Eventually(t, func() bool { return fake.count(profileKindHeap) >= 2 },
		2*time.Second, time.Millisecond)
	trail := stop()

	require.NotNil(t, trail)
	assert.Positive(t, trail.Unread)
	for _, row := range trail.Growth {
		assert.NotEqual(t, profileKindHeap, row.Kind)
	}
}

// A run that was never asked to watch says nothing, rather than saying it
// watched and found nothing.
func TestNoWatchIsAskedForWhenNoIntervalIsDeclared(t *testing.T) {
	assert.Nil(t, WatchForLeaks("http://127.0.0.1:8081", t.TempDir(), 0)())
	assert.Nil(t, WatchForLeaks("", t.TempDir(), time.Minute)())
	assert.Nil(t, WatchForLeaks("http://127.0.0.1:8081", "", time.Minute)())
}

func TestLeakTrailTravelsInTheBundle(t *testing.T) {
	bundle := completeBundle()
	bundle.Leak = &LeakTrail{
		IntervalSeconds: 300,
		Snapshots:       61,
		Directory:       "profiles",
		Growth: []StackGrowth{{
			Kind: profileKindGoroutine, Unit: "goroutines",
			Site:  "relay.(*Relay).pump /src/server/internal/relay/relay.go:118",
			Delta: 7148, First: 0, Last: 7148, IntervalsGrown: 60, Intervals: 60,
		}},
	}
	require.NoError(t, bundle.Validate())

	dir := t.TempDir()
	_, err := bundle.WriteTo(dir)
	require.NoError(t, err)

	reloaded, err := LoadBundle(filepath.Join(dir, bundleFileName))
	require.NoError(t, err)
	require.NotNil(t, reloaded.Leak)
	assert.Equal(t, 61, reloaded.Leak.Snapshots)
	require.Len(t, reloaded.Leak.Growth, 1)
	assert.Equal(t, float64(7148), reloaded.Leak.Growth[0].Delta)
}

// A trail of one reading has no difference in it, so it cannot report that
// nothing grew. It is the same rule the breaking point already carries: an
// absence is only a finding once something proves it looked.
func TestATrailOfFewerThanTwoReadingsIsRefused(t *testing.T) {
	bundle := completeBundle()
	bundle.Leak = &LeakTrail{IntervalSeconds: 300, Snapshots: 1, Directory: "profiles"}
	require.ErrorContains(t, bundle.Validate(), "leak_trail")

	bundle.Leak = &LeakTrail{IntervalSeconds: 0, Snapshots: 9, Directory: "profiles"}
	require.ErrorContains(t, bundle.Validate(), "leak_trail")

	bundle.Leak = &LeakTrail{IntervalSeconds: 300, Snapshots: 9, Directory: ""}
	require.ErrorContains(t, bundle.Validate(), "leak_trail")
}

// The trail has to reach the bundle. Every other reading in this file is
// worthless if the run builds one and drops it on the way out.
func TestTheRunBundleCarriesTheTrailTheWatchProduced(t *testing.T) {
	in := runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
		Leak: &LeakTrail{
			IntervalSeconds: 300, Snapshots: 61, Directory: profileDirName,
			Growth: []StackGrowth{{Kind: profileKindGoroutine, Site: "relay.go:118", Delta: 12}},
		},
	}

	bundle := buildRunBundle(in)
	require.NotNil(t, bundle.Leak)
	assert.Equal(t, 61, bundle.Leak.Snapshots)

	// And a run nobody asked to watch carries none, rather than an empty one.
	in.Leak = nil
	assert.Nil(t, buildRunBundle(in).Leak)
}
