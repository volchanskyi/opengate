package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RangeAgg selects how QueryRange collapses the raw 10 s samples in each step
// bucket. Only avg/min/max are allowed — a chart band is honestly the min/max
// across the 10 s averages in the bucket, never fabricated host extrema.
type RangeAgg string

// Supported per-bucket aggregations for QueryRange.
const (
	RangeAvg RangeAgg = "avg"
	RangeMin RangeAgg = "min"
	RangeMax RangeAgg = "max"
)

func (a RangeAgg) overTimeFunc() (string, bool) {
	switch a {
	case RangeAvg:
		return "avg_over_time", true
	case RangeMin:
		return "min_over_time", true
	case RangeMax:
		return "max_over_time", true
	default:
		return "", false
	}
}

// RangeQuery describes a bounded, org-scoped, step-downsampled range read of a
// single metric. Matchers must not include org_id (the scoped client owns it).
type RangeQuery struct {
	Metric   string
	Matchers map[string]string
	Agg      RangeAgg
	Start    time.Time
	End      time.Time
	Step     time.Duration
}

// RangeSeries is one downsampled series. Timestamps are unix seconds ascending
// and align 1:1 with Values, ready to become a charting engine's aligned data.
type RangeSeries struct {
	Labels     map[string]string `json:"labels"`
	Timestamps []int64           `json:"timestamps"`
	Values     []float64         `json:"values"`
}

// InstantValue is one series' latest scalar from QueryInstant.
type InstantValue struct {
	Labels map[string]string
	TS     int64
	Value  float64
}

type vmMatrixResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

// QueryRange runs a bounded, org-scoped, step-downsampled range query for a
// single metric filtered by non-reserved label matchers. Each step bucket is
// collapsed with agg over the raw 10 s samples, so the returned point count per
// series is bounded by (end-start)/step regardless of window span — the core
// scalability lever. The authoritative org_id matcher is injected here; callers
// never supply their own.
func (v *VMClient) QueryRange(ctx context.Context, orgID uuid.UUID, rq RangeQuery) ([]RangeSeries, error) {
	fn, ok := rq.Agg.overTimeFunc()
	if !ok {
		return nil, fmt.Errorf("unsupported range aggregation %q", rq.Agg)
	}
	if rq.Step <= 0 {
		return nil, fmt.Errorf("step must be positive")
	}
	scoped, err := v.scopedSelector(orgID, rq.Metric, rq.Matchers)
	if err != nil {
		return nil, err
	}
	stepSecs := max(int64(rq.Step.Seconds()), 1)
	q := url.Values{}
	q.Set("query", fmt.Sprintf("%s(%s[%ds])", fn, scoped, stepSecs))
	q.Set("start", strconv.FormatInt(rq.Start.Unix(), 10))
	q.Set("end", strconv.FormatInt(rq.End.Unix(), 10))
	q.Set("step", strconv.FormatInt(stepSecs, 10)+"s")

	resp, err := v.getMatrix(ctx, "/api/v1/query_range?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return matrixToRangeSeries(resp)
}

// QueryInstant runs an org-scoped instant query returning the latest value per
// series for the metric filtered by matchers. Passing no device_id matcher
// yields one value per device in the org — a single query behind the fleet
// health badge.
func (v *VMClient) QueryInstant(ctx context.Context, orgID uuid.UUID, metric string, matchers map[string]string, at time.Time) ([]InstantValue, error) {
	scoped, err := v.scopedSelector(orgID, metric, matchers)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("query", scoped)
	q.Set("time", strconv.FormatInt(at.Unix(), 10))

	resp, err := v.getMatrix(ctx, "/api/v1/query?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return vectorToInstant(resp)
}

// scopedSelector builds a validated `metric{...}` selector and injects the
// authoritative org_id matcher — the single choke point both read paths share.
func (v *VMClient) scopedSelector(orgID uuid.UUID, metric string, matchers map[string]string) (string, error) {
	selector, err := buildSelector(metric, matchers)
	if err != nil {
		return "", err
	}
	return ScopeSelector(selector, orgID)
}

func (v *VMClient) getMatrix(ctx context.Context, path string) (*vmMatrixResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("build vm query request: %w", err)
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get vm query: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("vm query status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out vmMatrixResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode vm query: %w", err)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("vm query status %q", out.Status)
	}
	return &out, nil
}

func matrixToRangeSeries(resp *vmMatrixResponse) ([]RangeSeries, error) {
	out := make([]RangeSeries, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		s := RangeSeries{
			Labels:     r.Metric,
			Timestamps: make([]int64, 0, len(r.Values)),
			Values:     make([]float64, 0, len(r.Values)),
		}
		for _, pair := range r.Values {
			ts, val, err := parseVMSample(pair)
			if err != nil {
				return nil, err
			}
			s.Timestamps = append(s.Timestamps, ts)
			s.Values = append(s.Values, val)
		}
		out = append(out, s)
	}
	return out, nil
}

func vectorToInstant(resp *vmMatrixResponse) ([]InstantValue, error) {
	out := make([]InstantValue, 0, len(resp.Data.Result))
	for _, r := range resp.Data.Result {
		ts, val, err := parseVMSample(r.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, InstantValue{Labels: r.Metric, TS: ts, Value: val})
	}
	return out, nil
}

// parseVMSample decodes a Prometheus [<unix seconds>, "<value>"] pair.
func parseVMSample(pair [2]any) (int64, float64, error) {
	tsFloat, ok := pair[0].(float64)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected vm timestamp type %T", pair[0])
	}
	valStr, ok := pair[1].(string)
	if !ok {
		return 0, 0, fmt.Errorf("unexpected vm value type %T", pair[1])
	}
	val, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse vm value %q: %w", valStr, err)
	}
	return int64(tsFloat), val, nil
}

// buildSelector composes a validated `metric{k="v",...}` selector with sorted,
// escaped matchers. It rejects an org_id matcher (the scoped client owns it) and
// invalid metric/label names.
func buildSelector(metric string, matchers map[string]string) (string, error) {
	if !metricNameRE.MatchString(metric) {
		return "", fmt.Errorf("invalid metric name %q", metric)
	}
	if _, ok := matchers["org_id"]; ok {
		return "", ErrOrgMatcherNotAllowed
	}
	if len(matchers) == 0 {
		return metric, nil
	}
	keys := make([]string, 0, len(matchers))
	for k := range matchers {
		if !labelNameRE.MatchString(k) {
			return "", fmt.Errorf("invalid label name %q", k)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(metric)
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `%s="%s"`, k, escapeLabelValue(matchers[k]))
	}
	b.WriteByte('}')
	return b.String(), nil
}
