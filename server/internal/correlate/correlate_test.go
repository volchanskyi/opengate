package correlate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeFetcher is an in-memory SeriesFetcher for engine tests. It records the
// scope it was called with and can block or fail on demand.
type fakeFetcher struct {
	series []Series
	err    error

	mu          sync.Mutex
	gotOrg      uuid.UUID
	gotDevice   uuid.UUID
	gotStart    time.Time
	gotEnd      time.Time
	calls       int
	block       chan struct{} // if non-nil, Fetch blocks until closed or ctx done
	respectsCtx bool          // if true, Fetch returns ctx.Err() when ctx cancels
}

func (f *fakeFetcher) Fetch(ctx context.Context, orgID, deviceID uuid.UUID, start, end time.Time) ([]Series, error) {
	f.mu.Lock()
	f.gotOrg, f.gotDevice, f.gotStart, f.gotEnd = orgID, deviceID, start, end
	f.calls++
	f.mu.Unlock()
	if f.block != nil {
		if f.respectsCtx {
			select {
			case <-f.block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		} else {
			<-f.block
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.series, nil
}

var baseT = time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)

// window helpers: baseline is [baseT, baseT+10m), focus is [baseT+10m, baseT+20m].
func baselineStart() time.Time { return baseT }
func focusStart() time.Time    { return baseT.Add(10 * time.Minute) }
func focusEnd() time.Time      { return baseT.Add(20 * time.Minute) }

// seriesWith builds a series whose baseline points carry baseVal and whose focus
// points carry focusVal, one per minute in each window.
func seriesWith(metric string, baseVal, focusVal float64) Series {
	s := Series{Metric: metric, Labels: map[string]string{"__name__": metric}}
	for i := range 10 {
		s.Points = append(s.Points, Point{TS: baseT.Add(time.Duration(i) * time.Minute), Value: baseVal})
	}
	for i := range 10 {
		off := 10 + i
		s.Points = append(s.Points, Point{TS: baseT.Add(time.Duration(off) * time.Minute), Value: focusVal})
	}
	return s
}

func newTestEngine(t *testing.T, f SeriesFetcher, mut func(*Config)) *Engine {
	t.Helper()
	cfg := Config{Fetcher: f}
	if mut != nil {
		mut(&cfg)
	}
	e, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return e
}

func stdRequest() Request {
	return Request{
		DeviceID:      uuid.New(),
		BaselineStart: baselineStart(),
		BaselineEnd:   focusStart(),
		FocusStart:    focusStart(),
		FocusEnd:      focusEnd(),
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func TestNewEngineRequiresFetcher(t *testing.T) {
	t.Parallel()
	if _, err := NewEngine(Config{}); !errors.Is(err, ErrNoFetcher) {
		t.Fatalf("NewEngine without fetcher: got %v, want ErrNoFetcher", err)
	}
}
