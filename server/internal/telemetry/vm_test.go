package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

func newTestVMClient(t *testing.T) (*VMClient, context.Context) {
	t.Helper()
	return NewVMClient(testvm.BaseURL(t), nil), context.Background()
}

func writeAnomalyRate(t *testing.T, c *VMClient, ctx context.Context, tenant, device uuid.UUID, value float64, ts time.Time) {
	t.Helper()
	require.NoError(t, c.WriteSamples(ctx, tenant, device, []Sample{{
		Name: "opengate_edge_node_anomaly_rate", Value: value, TS: ts,
	}}))
	require.NoError(t, c.Flush(ctx))
}

func writeDimSample(t *testing.T, c *VMClient, ctx context.Context, tenant, device uuid.UUID, s Sample, dim string) {
	t.Helper()
	s.Labels = map[string]string{"dim": dim}
	require.NoError(t, c.WriteSamples(ctx, tenant, device, []Sample{s}))
}

func TestVMClientWritesTenantScopedSamples(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	deviceID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	writeDimSample(t, client, ctx, tenantA, deviceID, Sample{Name: "opengate_test_ws4_metric", Value: 41, TS: ts}, "cpu")
	writeDimSample(t, client, ctx, tenantB, deviceID, Sample{Name: "opengate_test_ws4_metric", Value: 82, TS: ts}, "cpu")
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
	end := time.Now().UTC().Truncate(10 * time.Second)
	start := end.Add(-10 * time.Minute)
	for i := 0; ; i++ {
		ts := start.Add(time.Duration(i) * 10 * time.Second)
		if ts.After(end) {
			break
		}
		writeDimSample(t, client, ctx, tenantA, deviceID, Sample{Name: "opengate_edge_metric_avg", Value: float64(i), TS: ts}, "cpu.util")
		writeDimSample(t, client, ctx, tenantB, deviceID, Sample{Name: "opengate_edge_metric_avg", Value: 999, TS: ts}, "cpu.util")
	}
	require.NoError(t, client.Flush(ctx))

	rangeQuery := func(agg RangeAgg) RangeQuery {
		return RangeQuery{
			Metric:   "opengate_edge_metric_avg",
			Matchers: map[string]string{"device_id": deviceID.String(), "dim": "cpu.util"},
			Agg:      agg, Start: start, End: end, Step: time.Minute,
		}
	}
	series, err := client.QueryRange(ctx, tenantA, rangeQuery(RangeAvg))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, tenantA.String(), series[0].Labels["tenant_id"])
	assert.LessOrEqual(t, len(series[0].Values), 12, "step must bound the point count")
	assert.Positive(t, len(series[0].Values))
	require.Len(t, series[0].Timestamps, len(series[0].Values))
	for _, v := range series[0].Values {
		assert.Less(t, v, 999.0)
	}

	maxSeries, err := client.QueryRange(ctx, tenantA, rangeQuery(RangeMax))
	require.NoError(t, err)
	require.Len(t, maxSeries, 1)
	assert.GreaterOrEqual(t, maxSeries[0].Values[len(maxSeries[0].Values)-1], series[0].Values[len(series[0].Values)-1])
}

func TestVMClientQueryInstantLookbackSurfacesRecentSample(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenant := uuid.New()
	deviceID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-8 * time.Minute)

	writeAnomalyRate(t, client, ctx, tenant, deviceID, 0.33, old)

	bare, err := client.QueryInstant(ctx, tenant, "opengate_edge_node_anomaly_rate", nil, now)
	require.NoError(t, err)
	assert.Empty(t, bare, "an 8-minute-old sample is stale for a bare instant query")

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
	const metric = "opengate_edge_metric_avg"
	tests := []struct {
		name  string
		query RangeQuery
		errIs error
	}{
		{"zero step", RangeQuery{Metric: metric, Agg: RangeAvg, Start: start, End: end, Step: 0}, nil},
		{"tenant matcher", RangeQuery{Metric: metric, Matchers: map[string]string{"tenant_id": "x"}, Agg: RangeAvg, Start: start, End: end, Step: time.Minute}, ErrTenantMatcherNotAllowed},
		{"invalid metric name", RangeQuery{Metric: "bad name", Agg: RangeAvg, Start: start, End: end, Step: time.Minute}, nil},
		{"unsupported aggregation", RangeQuery{Metric: metric, Agg: RangeAgg("sum"), Start: start, End: end, Step: time.Minute}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.QueryRange(ctx, uuid.New(), tc.query)
			require.Error(t, err)
			if tc.errIs != nil {
				require.ErrorIs(t, err, tc.errIs)
			}
		})
	}
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

	// VM applies a 30 s search latency offset, so the query evaluates past it.
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
