package correlate

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCorrelateRanksInjectedAnomalyFirst(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{
		seriesWith("cpu_pct", 10, 10),    // flat — no shift
		seriesWith("mem_pct", 10, 500),   // large shift — the injected anomaly
		seriesWith("disk_pct", 10, 10.5), // tiny in-band drift
	}}
	e := newTestEngine(t, f, nil)

	res, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if len(res.Ranked) == 0 {
		t.Fatal("no ranked dimensions returned")
	}
	top := res.Ranked[0]
	if top.Metric != "mem_pct" {
		t.Fatalf("top dimension = %q, want mem_pct", top.Metric)
	}
	if top.KSStatistic < 0.9 {
		t.Errorf("injected anomaly KS = %v, want >= 0.9", top.KSStatistic)
	}
	if top.AnomalyRate < 0.9 {
		t.Errorf("injected anomaly rate = %v, want >= 0.9", top.AnomalyRate)
	}
	// The flat dimension must rank strictly below the anomaly.
	last := res.Ranked[len(res.Ranked)-1]
	if last.Metric != "cpu_pct" {
		t.Errorf("bottom dimension = %q, want cpu_pct (flat)", last.Metric)
	}
	if last.Score >= top.Score {
		t.Errorf("flat score %v not below anomaly score %v", last.Score, top.Score)
	}
}

func TestCorrelateTopNCaps(t *testing.T) {
	t.Parallel()
	f := &fakeFetcher{series: []Series{
		seriesWith("a", 10, 500), seriesWith("b", 10, 400),
		seriesWith("c", 10, 300), seriesWith("d", 10, 200),
	}}
	e := newTestEngine(t, f, nil)
	req := stdRequest()
	req.TopN = 2
	res, err := e.Correlate(context.Background(), uuid.New(), req)
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if len(res.Ranked) != 2 {
		t.Fatalf("TopN=2 returned %d dimensions", len(res.Ranked))
	}
}

func TestCorrelateSkipsThinWindows(t *testing.T) {
	t.Parallel()
	// A series with only a single baseline point cannot be tested for a shift.
	thin := Series{Metric: "thin", Labels: map[string]string{"__name__": "thin"}, Points: []Point{
		{TS: baseT, Value: 1},
		{TS: focusStart().Add(time.Minute), Value: 99},
	}}
	f := &fakeFetcher{series: []Series{thin, seriesWith("real", 10, 500)}}
	e := newTestEngine(t, f, nil)
	res, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	for _, r := range res.Ranked {
		if r.Metric == "thin" {
			t.Fatalf("thin-window series should be skipped, got %+v", r)
		}
	}
}

func TestCorrelateTruncatesExcessSeries(t *testing.T) {
	t.Parallel()
	var many []Series
	for i := range 10 {
		many = append(many, seriesWith(string(rune('a'+i)), 10, float64(100+i)))
	}
	f := &fakeFetcher{series: many}
	e := newTestEngine(t, f, func(c *Config) { c.MaxSeries = 3 })
	res, err := e.Correlate(context.Background(), uuid.New(), stdRequest())
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !res.SeriesTruncated {
		t.Error("SeriesTruncated = false, want true when series exceed MaxSeries")
	}
	if res.SeriesConsidered != 3 {
		t.Errorf("SeriesConsidered = %d, want 3", res.SeriesConsidered)
	}
}
