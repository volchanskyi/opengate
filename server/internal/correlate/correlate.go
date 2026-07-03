// Package correlate implements OpenGate's on-demand metric-correlation engine.
//
// Given a tenant, a device, and an incident (focus) window, it fetches the
// device's candidate numeric series from VictoriaMetrics through the WS-4
// tenant-scoped read path and ranks the dimensions that "broke pattern" versus a
// baseline window. Ranking is a two-sample Kolmogorov–Smirnov distribution-shift
// statistic combined with an anomaly-rate volume signal, computed server-side in
// Go (VictoriaMetrics' MetricsQL has no native KS test or SQL join).
//
// The engine is on-demand only — there is no background correlation matrix. Each
// request is bounded by a concurrency limiter, a per-request timeout, and caps on
// the number of series and points considered, so a correlation can never starve
// the control plane.
package correlate

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Engine defaults. Every value is overridable through Config.
const (
	defaultTopN               = 20
	defaultMaxConcurrent      = 4
	defaultTimeout            = 15 * time.Second
	defaultMaxSeries          = 2000
	defaultMaxPointsPerSeries = 5000
	// minWindowSamples is the fewest points a series needs in each window before
	// a shift can be judged; below it the series is skipped (cold-start safe).
	minWindowSamples = 2
	// ksWeight / anomalyWeight / magnitudeWeight blend the three co-signals into
	// the rank score (they sum to 1): distribution shift (KS), how many focus
	// points broke the baseline band (anomaly rate), and how far the mean moved
	// relative to baseline scale (shift magnitude / volume).
	ksWeight        = 0.4
	anomalyWeight   = 0.3
	magnitudeWeight = 0.3
)

// Engine errors.
var (
	// ErrNoFetcher is returned by NewEngine when Config has no SeriesFetcher.
	ErrNoFetcher = errors.New("correlate: no series fetcher configured")
	// ErrBusy is returned when the concurrency limit is already saturated.
	ErrBusy = errors.New("correlate: concurrency limit reached")
	// ErrInvalidWindow is returned for a non-positive or malformed window.
	ErrInvalidWindow = errors.New("correlate: invalid window")
)

// Point is a single timestamped numeric sample.
type Point struct {
	TS    time.Time
	Value float64
}

// Series is one candidate metric dimension: its identifying labels and its
// points across the fetched window.
type Series struct {
	Metric string
	Labels map[string]string
	Points []Point
}

// SeriesFetcher fetches all candidate numeric series for a device over a window.
// Implementations MUST inject the tenant org_id label matcher and never trust a
// caller-supplied scope — it is the isolation boundary for numeric telemetry.
type SeriesFetcher interface {
	Fetch(ctx context.Context, orgID, deviceID uuid.UUID, start, end time.Time) ([]Series, error)
}

// Request is one correlation query.
type Request struct {
	DeviceID   uuid.UUID
	FocusStart time.Time
	FocusEnd   time.Time
	// BaselineStart/BaselineEnd are optional. When BaselineStart is zero the
	// baseline defaults to the window of equal length immediately preceding focus.
	BaselineStart time.Time
	BaselineEnd   time.Time
	// TopN caps the returned dimensions; zero uses the engine default.
	TopN int
}

// Ranked is one scored dimension in a correlation result.
type Ranked struct {
	Metric          string
	Labels          map[string]string
	Score           float64
	KSStatistic     float64
	AnomalyRate     float64
	ShiftMagnitude  float64
	BaselineSamples int
	FocusSamples    int
}

// Result is the ranked correlation output.
type Result struct {
	Ranked           []Ranked
	SeriesConsidered int
	SeriesTruncated  bool
}

// Config configures an Engine. Only Fetcher is required.
type Config struct {
	Fetcher            SeriesFetcher
	MaxConcurrent      int
	Timeout            time.Duration
	MaxSeries          int
	MaxPointsPerSeries int
	DefaultTopN        int
}

// Engine ranks anomalous metric dimensions on demand.
type Engine struct {
	fetcher            SeriesFetcher
	sem                chan struct{}
	timeout            time.Duration
	maxSeries          int
	maxPointsPerSeries int
	defaultTopN        int
}

// NewEngine builds an Engine, applying defaults for any unset Config field.
func NewEngine(cfg Config) (*Engine, error) {
	if cfg.Fetcher == nil {
		return nil, ErrNoFetcher
	}
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = defaultMaxConcurrent
	}
	e := &Engine{
		fetcher:            cfg.Fetcher,
		sem:                make(chan struct{}, maxConcurrent),
		timeout:            orDurationDefault(cfg.Timeout, defaultTimeout),
		maxSeries:          orIntDefault(cfg.MaxSeries, defaultMaxSeries),
		maxPointsPerSeries: orIntDefault(cfg.MaxPointsPerSeries, defaultMaxPointsPerSeries),
		defaultTopN:        orIntDefault(cfg.DefaultTopN, defaultTopN),
	}
	return e, nil
}

// Correlate ranks the device's dimensions for the request window under the given
// tenant. It acquires a concurrency slot (returning ErrBusy if saturated) and
// runs under a per-request timeout. orgID scopes every VM read; it is taken from
// the authenticated tenant context, never from the request body.
func (e *Engine) Correlate(ctx context.Context, orgID uuid.UUID, req Request) (Result, error) {
	req, err := normalizeRequest(req)
	if err != nil {
		return Result{}, err
	}
	if req.TopN <= 0 {
		req.TopN = e.defaultTopN
	}

	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		return Result{}, ErrBusy
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	series, err := e.fetcher.Fetch(ctx, orgID, req.DeviceID, req.BaselineStart, req.FocusEnd)
	if err != nil {
		return Result{}, err
	}

	res := Result{}
	if len(series) > e.maxSeries {
		series = series[:e.maxSeries]
		res.SeriesTruncated = true
	}
	res.SeriesConsidered = len(series)

	ranked := make([]Ranked, 0, len(series))
	for _, s := range series {
		if r, ok := e.rankSeries(s, req); ok {
			ranked = append(ranked, r)
		}
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		if ranked[i].KSStatistic != ranked[j].KSStatistic {
			return ranked[i].KSStatistic > ranked[j].KSStatistic
		}
		return ranked[i].Metric < ranked[j].Metric
	})
	if len(ranked) > req.TopN {
		ranked = ranked[:req.TopN]
	}
	res.Ranked = ranked
	return res, nil
}

// rankSeries splits one series into baseline/focus samples and scores it. It
// returns ok=false when either window has too few samples to judge a shift.
func (e *Engine) rankSeries(s Series, req Request) (Ranked, bool) {
	baseline := make([]float64, 0, e.maxPointsPerSeries)
	focus := make([]float64, 0, e.maxPointsPerSeries)
	for _, p := range s.Points {
		switch {
		case !p.TS.Before(req.BaselineStart) && p.TS.Before(req.BaselineEnd):
			if len(baseline) < e.maxPointsPerSeries {
				baseline = append(baseline, p.Value)
			}
		case !p.TS.Before(req.FocusStart) && !p.TS.After(req.FocusEnd):
			if len(focus) < e.maxPointsPerSeries {
				focus = append(focus, p.Value)
			}
		}
	}
	if len(baseline) < minWindowSamples || len(focus) < minWindowSamples {
		return Ranked{}, false
	}
	ks := ksStatistic(baseline, focus)
	ar := anomalyRate(baseline, focus)
	mag := shiftMagnitude(baseline, focus)
	return Ranked{
		Metric:          s.Metric,
		Labels:          s.Labels,
		Score:           ksWeight*ks + anomalyWeight*clamp01(ar) + magnitudeWeight*mag,
		KSStatistic:     ks,
		AnomalyRate:     ar,
		ShiftMagnitude:  mag,
		BaselineSamples: len(baseline),
		FocusSamples:    len(focus),
	}, true
}

// normalizeRequest validates the focus window and fills the default baseline and
// TopN.
func normalizeRequest(req Request) (Request, error) {
	if req.FocusStart.IsZero() || req.FocusEnd.IsZero() || !req.FocusStart.Before(req.FocusEnd) {
		return Request{}, ErrInvalidWindow
	}
	if req.BaselineStart.IsZero() {
		width := req.FocusEnd.Sub(req.FocusStart)
		req.BaselineStart = req.FocusStart.Add(-width)
		req.BaselineEnd = req.FocusStart
	}
	if req.BaselineEnd.IsZero() {
		req.BaselineEnd = req.FocusStart
	}
	if !req.BaselineStart.Before(req.BaselineEnd) {
		return Request{}, ErrInvalidWindow
	}
	if req.TopN <= 0 {
		req.TopN = defaultTopN
	}
	return req, nil
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func orIntDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

func orDurationDefault(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}
