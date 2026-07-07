package correlate

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// This file pins the window/cap/ordering boundaries of the correlation engine
// (Engine.rankSeries, Engine.Correlate) and the VM fetcher that the mutation
// suite flags.

// seriesWithCounts builds a series with exactly nBase baseline points and nFocus
// focus points, each carrying a distinct value so the windows are non-degenerate.
func seriesWithCounts(metric string, nBase, nFocus int) Series {
	s := Series{Metric: metric, Labels: map[string]string{"__name__": metric}}
	for i := range nBase {
		s.Points = append(s.Points, Point{TS: baseT.Add(time.Duration(i) * time.Minute), Value: float64(10 + i)})
	}
	for i := range nFocus {
		s.Points = append(s.Points, Point{TS: focusStart().Add(time.Duration(i) * time.Minute), Value: float64(500 + i)})
	}
	return s
}

// rankSeries scores a series only when both windows carry at least
// minWindowSamples points. Exactly minWindowSamples must pass; one fewer in
// either window must be skipped.
func TestRankSeriesMinWindowBoundary(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, &fakeFetcher{}, nil)
	req := stdRequest()

	if _, ok := e.rankSeries(seriesWithCounts("exact", minWindowSamples, minWindowSamples), req); !ok {
		t.Fatalf("exactly minWindowSamples in each window must be ranked")
	}
	if _, ok := e.rankSeries(seriesWithCounts("thinbase", minWindowSamples-1, 5), req); ok {
		t.Fatalf("below minWindowSamples baseline must be skipped")
	}
	if _, ok := e.rankSeries(seriesWithCounts("thinfocus", 5, minWindowSamples-1), req); ok {
		t.Fatalf("below minWindowSamples focus must be skipped")
	}
}

// rankSeries caps the retained points per window at maxPointsPerSeries. With a
// cap of 3 and 10 points per window, exactly 3 are retained.
func TestRankSeriesCapsPointsPerWindow(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, &fakeFetcher{}, func(c *Config) { c.MaxPointsPerSeries = 3 })
	r, ok := e.rankSeries(seriesWithCounts("capped", 10, 10), stdRequest())
	if !ok {
		t.Fatalf("series must be ranked")
	}
	if r.BaselineSamples != 3 {
		t.Fatalf("BaselineSamples = %d, want 3 (capped)", r.BaselineSamples)
	}
	if r.FocusSamples != 3 {
		t.Fatalf("FocusSamples = %d, want 3 (capped)", r.FocusSamples)
	}
}

// The rank score is the exact weighted blend of the three co-signals.
func TestRankSeriesScoreIsWeightedBlend(t *testing.T) {
	t.Parallel()
	e := newTestEngine(t, &fakeFetcher{}, nil)
	r, ok := e.rankSeries(seriesWith("blend", 10, 500), stdRequest())
	if !ok {
		t.Fatalf("series must be ranked")
	}
	want := ksWeight*r.KSStatistic + anomalyWeight*clamp01(r.AnomalyRate) + magnitudeWeight*r.ShiftMagnitude
	if math.Abs(r.Score-want) > 1e-12 {
		t.Fatalf("Score = %v, want weighted blend %v", r.Score, want)
	}
}

// Exactly MaxSeries candidate series must not be truncated; the `>` guard only
// trips above the cap.
func TestCorrelateNotTruncatedAtExactMaxSeries(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{
		seriesWith("a", 10, 500), seriesWith("b", 10, 400), seriesWith("c", 10, 300),
	}}
	e := newTestEngine(t, f, func(c *Config) { c.MaxSeries = 3 })
	res, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if res.SeriesTruncated {
		t.Fatalf("exactly MaxSeries must not be truncated")
	}
	if res.SeriesConsidered != 3 {
		t.Fatalf("SeriesConsidered = %d, want 3", res.SeriesConsidered)
	}
}

// A series with an empty Metric map must yield nil dimension labels: the
// `len(es.Metric) > 0` guard skips the map allocation.
func TestVMFetcherEmptyMetricLeavesLabelsNil(t *testing.T) {
	t.Parallel()
	stub := &stubExporter{series: []telemetry.ExportedSeries{
		{Metric: map[string]string{}, Values: []float64{1, 2}, Timestamps: []int64{100, 200}},
	}}
	f := NewVMFetcher(stub)
	got, err := f.Fetch(context.Background(), uuid.New(), uuid.New(), time.Now(), time.Now())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d series, want 1", len(got))
	}
	if got[0].Labels != nil {
		t.Fatalf("empty Metric must leave Labels nil, got %+v", got[0].Labels)
	}
}

// When Score and KS tie, results order by ascending metric name. Two identical
// series (different names) must come back name-sorted.
func TestCorrelateTieBreaksByMetricName(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{
		seriesWith("zebra", 10, 500), seriesWith("alpha", 10, 500),
	}}
	e := newTestEngine(t, f, nil)
	res, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if len(res.Ranked) != 2 {
		t.Fatalf("got %d ranked, want 2", len(res.Ranked))
	}
	if res.Ranked[0].Metric != "alpha" {
		t.Fatalf("tie-break order[0] = %q, want alpha (ascending name)", res.Ranked[0].Metric)
	}
}
