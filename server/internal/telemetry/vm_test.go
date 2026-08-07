package telemetry

import (
	"bytes"
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// newTestVMClient builds a VM client against the throwaway test VictoriaMetrics
// with a background context, sparing every test the same three-line setup.
func newTestVMClient(t *testing.T) (*VMClient, context.Context) {
	t.Helper()
	return NewVMClient(testvm.BaseURL(t), nil), context.Background()
}

// writeAnomalyRate writes and flushes one node anomaly-rate sample for a device,
// the shape the fleet-health badge reads.
func writeAnomalyRate(t *testing.T, c *VMClient, ctx context.Context, tenant, device uuid.UUID, value float64, ts time.Time) {
	t.Helper()
	require.NoError(t, c.WriteSamples(ctx, tenant, device, []Sample{{
		Name: "opengate_edge_node_anomaly_rate", Value: value, TS: ts,
	}}))
	require.NoError(t, c.Flush(ctx))
}

func TestVMClientWritesTenantScopedSamples(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	deviceID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, client.WriteSamples(ctx, tenantA, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  41,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.WriteSamples(ctx, tenantB, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  82,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, tenantA, `opengate_test_ws4_metric{device_id="`+deviceID.String()+`"}`, ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, tenantA.String(), series[0].Metric["tenant_id"])
	assert.Equal(t, []float64{41}, series[0].Values)
}

func TestVMClientQueryRangeDownsamplesAndScopes(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	deviceID := uuid.New()
	// Ten-second-avg samples across a 10-minute window: 60 raw points.
	end := time.Now().UTC().Truncate(10 * time.Second)
	start := end.Add(-10 * time.Minute)
	for i := 0; ; i++ {
		ts := start.Add(time.Duration(i) * 10 * time.Second)
		if ts.After(end) {
			break
		}
		require.NoError(t, client.WriteSamples(ctx, tenantA, deviceID, []Sample{{
			Name: "opengate_edge_metric_avg", Value: float64(i), TS: ts,
			Labels: map[string]string{"dim": "cpu.util"},
		}}))
		require.NoError(t, client.WriteSamples(ctx, tenantB, deviceID, []Sample{{
			Name: "opengate_edge_metric_avg", Value: 999, TS: ts,
			Labels: map[string]string{"dim": "cpu.util"},
		}}))
	}
	require.NoError(t, client.Flush(ctx))

	// One-minute step over ten minutes → at most ~11 points, never the 60 raw.
	series, err := client.QueryRange(ctx, tenantA, RangeQuery{
		Metric:   "opengate_edge_metric_avg",
		Matchers: map[string]string{"device_id": deviceID.String(), "dim": "cpu.util"},
		Agg:      RangeAvg, Start: start, End: end, Step: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, tenantA.String(), series[0].Labels["tenant_id"])
	assert.LessOrEqual(t, len(series[0].Values), 12, "step must bound the point count")
	assert.Positive(t, len(series[0].Values))
	require.Len(t, series[0].Timestamps, len(series[0].Values))
	// tenantB's constant 999 never leaks into tenantA's downsampled averages.
	for _, v := range series[0].Values {
		assert.Less(t, v, 999.0)
	}

	// max aggregation returns the bucket peak, strictly above the avg.
	maxSeries, err := client.QueryRange(ctx, tenantA, RangeQuery{
		Metric:   "opengate_edge_metric_avg",
		Matchers: map[string]string{"device_id": deviceID.String(), "dim": "cpu.util"},
		Agg:      RangeMax, Start: start, End: end, Step: time.Minute,
	})
	require.NoError(t, err)
	require.Len(t, maxSeries, 1)
	assert.GreaterOrEqual(t, maxSeries[0].Values[len(maxSeries[0].Values)-1], series[0].Values[len(series[0].Values)-1])
}

// TestVMClientQueryInstantLookbackSurfacesRecentSample proves the badge's
// safety net: a node anomaly-rate sample a few minutes old is beyond VM's
// instant-query staleness window (so a bare instant query at now is empty), yet
// last_over_time over a 10 min window still resolves it.
func TestVMClientQueryInstantLookbackSurfacesRecentSample(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenant := uuid.New()
	deviceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-8 * time.Minute)

	writeAnomalyRate(t, client, ctx, tenant, deviceID, 0.33, old)

	// Bare instant query at now: the sample is past the staleness window → empty.
	bare, err := client.QueryInstant(ctx, tenant, "opengate_edge_node_anomaly_rate", nil, now)
	require.NoError(t, err)
	assert.Empty(t, bare, "an 8-minute-old sample is stale for a bare instant query")

	// Lookback query: last_over_time([10m]) still resolves the recent sample.
	vals, err := client.QueryInstantLookback(ctx, tenant, "opengate_edge_node_anomaly_rate", nil, now, 10*time.Minute)
	require.NoError(t, err)
	require.Len(t, vals, 1)
	assert.InDelta(t, 0.33, vals[0].Value, 0.0001)
}

func TestVMClientQueryRangeRejectsBadInput(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	ctx := context.Background()
	start := time.Unix(1_700_000_000, 0)
	end := start.Add(time.Hour)
	_, err := client.QueryRange(ctx, uuid.New(), RangeQuery{Metric: "opengate_edge_metric_avg", Agg: RangeAvg, Start: start, End: end, Step: 0})
	require.Error(t, err, "zero step must be rejected")
	_, err = client.QueryRange(ctx, uuid.New(), RangeQuery{Metric: "opengate_edge_metric_avg", Matchers: map[string]string{"tenant_id": "x"}, Agg: RangeAvg, Start: start, End: end, Step: time.Minute})
	require.ErrorIs(t, err, ErrTenantMatcherNotAllowed)
	_, err = client.QueryRange(ctx, uuid.New(), RangeQuery{Metric: "bad name", Agg: RangeAvg, Start: start, End: end, Step: time.Minute})
	require.Error(t, err, "invalid metric name must be rejected")
	_, err = client.QueryRange(ctx, uuid.New(), RangeQuery{Metric: "opengate_edge_metric_avg", Agg: RangeAgg("sum"), Start: start, End: end, Step: time.Minute})
	require.Error(t, err, "unsupported aggregation must be rejected")
}

func TestVMClientQueryInstantScopesToTenant(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	devA := uuid.New()
	devB := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	writeAnomalyRate(t, client, ctx, tenantA, devA, 0.42, ts)
	writeAnomalyRate(t, client, ctx, tenantA, devB, 0.13, ts)
	writeAnomalyRate(t, client, ctx, tenantB, devA, 0.99, ts)

	// VM applies a 30 s search latency offset, so evaluate past that boundary.
	vals, err := client.QueryInstant(ctx, tenantA, "opengate_edge_node_anomaly_rate", nil, ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, vals, 2, "both tenantA devices, neither tenantB")
	byDevice := map[string]float64{}
	for _, v := range vals {
		assert.Equal(t, tenantA.String(), v.Labels["tenant_id"])
		byDevice[v.Labels["device_id"]] = v.Value
	}
	assert.InDelta(t, 0.42, byDevice[devA.String()], 1e-9)
	assert.InDelta(t, 0.13, byDevice[devB.String()], 1e-9)
}

func TestScopeSelector(t *testing.T) {
	t.Parallel()
	const tenant = "11111111-1111-1111-1111-111111111111"
	tenantID := uuid.MustParse(tenant)
	tests := []struct {
		name     string
		selector string
		tenantID uuid.UUID
		want     string
		errIs    error
		wantErr  bool
	}{
		{name: "injects into existing label set", selector: `m{device_id="d1"}`, tenantID: tenantID, want: `m{tenant_id="` + tenant + `",device_id="d1"}`},
		{name: "injects into bare metric", selector: "m", tenantID: tenantID, want: `m{tenant_id="` + tenant + `"}`},
		{name: "injects into empty brace set", selector: "m{}", tenantID: tenantID, want: `m{tenant_id="` + tenant + `"}`},
		{name: "rejects caller-supplied tenant matcher", selector: `m{tenant_id="other"}`, tenantID: tenantID, errIs: ErrTenantMatcherNotAllowed},
		{name: "rejects nil tenant", selector: "m", tenantID: uuid.Nil, wantErr: true},
		{name: "rejects empty selector", selector: "   ", tenantID: tenantID, wantErr: true},
		{name: "rejects unterminated brace set", selector: "m{foo=", tenantID: tenantID, wantErr: true},
		{name: "rejects trailing open brace", selector: "m{", tenantID: tenantID, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScopeSelector(tt.selector, tt.tenantID)
			switch {
			case tt.errIs != nil:
				assert.ErrorIs(t, err, tt.errIs)
			case tt.wantErr:
				require.Error(t, err)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestWritePrometheusSample(t *testing.T) {
	t.Parallel()
	tenantID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	deviceID := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	ts := time.Unix(1_700_000_000, 0).UTC()
	tests := []struct {
		name     string
		sample   Sample
		want     string
		contains string
		errIs    error
		wantErr  bool
	}{
		{
			name:   "writes sorted label line",
			sample: Sample{Name: "opengate_edge_metric_avg", Value: 1.5, TS: ts, Labels: map[string]string{"dim": "cpu"}},
			want:   `opengate_edge_metric_avg{device_id="33333333-3333-3333-3333-333333333333",dim="cpu",tenant_id="22222222-2222-2222-2222-222222222222"} 1.5 1700000000000` + "\n",
		},
		{name: "rejects invalid metric name", sample: Sample{Name: "1bad name", Value: 1, TS: ts}, wantErr: true},
		{name: "rejects NaN", sample: Sample{Name: "m", Value: math.NaN(), TS: ts}, wantErr: true},
		{name: "rejects Inf", sample: Sample{Name: "m", Value: math.Inf(1), TS: ts}, wantErr: true},
		{name: "rejects reserved label", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"tenant_id": "x"}}, errIs: ErrReservedLabel},
		{name: "rejects invalid label name", sample: Sample{Name: "m", Value: 1, TS: ts, Labels: map[string]string{"bad-label": "x"}}, wantErr: true},
		{name: "defaults zero timestamp", sample: Sample{Name: "m", Value: 1}, contains: "m{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var b bytes.Buffer
			err := writePrometheusSample(&b, tenantID, deviceID, tt.sample)
			switch {
			case tt.errIs != nil:
				assert.ErrorIs(t, err, tt.errIs)
			case tt.wantErr:
				require.Error(t, err)
			case tt.contains != "":
				require.NoError(t, err)
				assert.Contains(t, b.String(), tt.contains)
			default:
				require.NoError(t, err)
				assert.Equal(t, tt.want, b.String())
			}
		})
	}
}

func TestEscapeLabelValue(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `a\\b\nc\"d`, escapeLabelValue("a\\b\nc\"d"))
	assert.Equal(t, "plain", escapeLabelValue("plain"))
}

func TestWriteSamplesEmptyIsNoop(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	require.NoError(t, client.WriteSamples(context.Background(), uuid.New(), uuid.New(), nil))
}
