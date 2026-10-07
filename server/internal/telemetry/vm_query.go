package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RangeAgg selects how QueryRange collapses the raw samples in each step bucket;
// only avg, min and max are allowed.
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

// RangeQuery describes a bounded, tenant-scoped, step-downsampled range read of a
// single metric. Matchers must not include tenant_id (the scoped client owns it).
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

// QueryRange runs a tenant-scoped range query for one metric, collapsing each step bucket
// with agg so points per series stay bounded by (end-start)/step. It injects tenant_id.
func (v *VMClient) QueryRange(ctx context.Context, tenantID uuid.UUID, rq RangeQuery) ([]RangeSeries, error) {
	fn, ok := rq.Agg.overTimeFunc()
	if !ok {
		return nil, fmt.Errorf("unsupported range aggregation %q", rq.Agg)
	}
	if rq.Step <= 0 {
		return nil, fmt.Errorf("step must be positive")
	}
	scoped, err := v.scopedSelector(tenantID, rq.Metric, rq.Matchers)
	if err != nil {
		return nil, err
	}
	stepSecs := max(int64(rq.Step.Seconds()), 1)
	q := url.Values{}
	q.Set("query", fmt.Sprintf("%s without (%s) (%s(%s[%ds]))", rq.Agg, environmentLabel, fn, scoped, stepSecs))
	q.Set("start", strconv.FormatInt(rq.Start.Unix(), 10))
	q.Set("end", strconv.FormatInt(rq.End.Unix(), 10))
	q.Set("step", strconv.FormatInt(stepSecs, 10)+"s")

	resp, err := v.getMatrix(ctx, "/api/v1/query_range?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return matrixToRangeSeries(resp)
}

// QueryInstant runs a tenant-scoped instant query returning the latest value per series;
// without a device_id matcher it yields one value per device in the tenant.
func (v *VMClient) QueryInstant(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time) ([]InstantValue, error) {
	return v.scopedInstant(ctx, tenantID, metric, matchers, at, func(selector string) string { return selector })
}

// QueryInstantLookback runs a tenant-scoped instant query returning the most recent value
// per series within lookback of at, so a gap between low-rate samples still resolves.
func (v *VMClient) QueryInstantLookback(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time, lookback time.Duration) ([]InstantValue, error) {
	return v.scopedInstant(ctx, tenantID, metric, matchers, at, func(selector string) string {
		return fmt.Sprintf("last_over_time(%s[%ds])", selector, int64(lookback.Seconds()))
	})
}

// BandCounts is how many devices fall in each edge-health band, counted inside
// VictoriaMetrics.
type BandCounts struct {
	Anomalous int
	Watch     int
	Healthy   int
}

// MetricNodeAnomalyRate is the per-device anomaly-rate gauge that drives both
// the per-device health badge and the fleet health bands.
const MetricNodeAnomalyRate = "opengate_edge_node_anomaly_rate"

// bandLabel names the synthetic label the band query stamps on each count so a
// single instant query can carry all three scalars back.
const bandLabel = "band"

// CountAnomalyBands returns the number of devices in each edge-health band for one tenant
// in a single instant query, with no per-device sample leaving VictoriaMetrics.
func (v *VMClient) CountAnomalyBands(ctx context.Context, tenantID uuid.UUID, watch, anomalous float64, at time.Time, lookback time.Duration) (BandCounts, error) {
	scoped, err := ScopeSelector(MetricNodeAnomalyRate, tenantID)
	if err != nil {
		return BandCounts{}, err
	}
	window := withoutEnvironment(fmt.Sprintf("last_over_time(%s[%ds])", scoped, int64(lookback.Seconds())))

	// One table builds the query and reads the answer, so label and field stay paired.
	var counts BandCounts
	bands := []struct {
		name      string
		predicate string
		into      *int
	}{
		{"anomalous", fmt.Sprintf(">= %s", formatThreshold(anomalous)), &counts.Anomalous},
		{"watch", fmt.Sprintf(">= %s < %s", formatThreshold(watch), formatThreshold(anomalous)), &counts.Watch},
		{"healthy", fmt.Sprintf("< %s", formatThreshold(watch)), &counts.Healthy},
	}

	parts := make([]string, 0, len(bands))
	fields := make(map[string]*int, len(bands))
	for _, b := range bands {
		parts = append(parts, fmt.Sprintf(`label_replace(count(%s %s), %q, %q, "", "")`,
			window, b.predicate, bandLabel, b.name))
		fields[b.name] = b.into
	}

	vals, err := v.instantQuery(ctx, strings.Join(parts, " or "), at)
	if err != nil {
		return BandCounts{}, err
	}

	// count() over an empty set yields no sample, so an absent band keeps its zero.
	for _, val := range vals {
		if field, ok := fields[val.Labels[bandLabel]]; ok {
			*field = int(val.Value)
		}
	}
	return counts, nil
}

// formatThreshold renders a band boundary as a plain PromQL literal, without
// scientific notation or a trailing exponent.
func formatThreshold(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// scopedInstant scopes the selector to tenantID, applies wrap, groups the environment
// away and runs an instant query at at.
func (v *VMClient) scopedInstant(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time, wrap func(string) string) ([]InstantValue, error) {
	scoped, err := v.scopedSelector(tenantID, metric, matchers)
	if err != nil {
		return nil, err
	}
	return v.instantQuery(ctx, withoutEnvironment(wrap(scoped)), at)
}

// withoutEnvironment groups an instant expression's series by everything but the
// environment stamp, so one device is one series.
func withoutEnvironment(expr string) string {
	return fmt.Sprintf("max without (%s) (%s)", environmentLabel, expr)
}

// instantQuery evaluates a PromQL expression at at and returns the latest value per series.
func (v *VMClient) instantQuery(ctx context.Context, expr string, at time.Time) ([]InstantValue, error) {
	q := url.Values{}
	q.Set("query", expr)
	q.Set("time", strconv.FormatInt(at.Unix(), 10))

	resp, err := v.getMatrix(ctx, "/api/v1/query?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return vectorToInstant(resp)
}

// scopedSelector builds a validated `metric{...}` selector and injects the
// authoritative tenant_id matcher — the single choke point both read paths share.
func (v *VMClient) scopedSelector(tenantID uuid.UUID, metric string, matchers map[string]string) (string, error) {
	selector, err := buildSelector(metric, matchers)
	if err != nil {
		return "", err
	}
	return ScopeSelector(selector, tenantID)
}

func (v *VMClient) getMatrix(ctx context.Context, path string) (*vmMatrixResponse, error) {
	resp, err := v.getChecked(ctx, path, "query")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
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

// buildSelector composes a `metric{k="v",...}` selector with sorted, escaped matchers,
// rejecting a tenant_id matcher and invalid metric or label names.
func buildSelector(metric string, matchers map[string]string) (string, error) {
	if !metricNameRE.MatchString(metric) {
		return "", fmt.Errorf("invalid metric name %q", metric)
	}
	if _, ok := matchers["tenant_id"]; ok {
		return "", ErrTenantMatcherNotAllowed
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
