package api

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

func newVMBackedServer(t *testing.T) (*Server, *appmetrics.Metrics) {
	t.Helper()
	srv, _ := newTestServer(t)
	srv.telemetryReader = telemetry.NewVMClient(testvm.BaseURL(t), nil)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	srv.metrics = m
	return srv, m
}

func writeRawWindow(t *testing.T, srv *Server, ctx context.Context, tenant, device uuid.UUID, start time.Time, span time.Duration) {
	t.Helper()
	client, ok := srv.telemetryReader.(*telemetry.VMClient)
	require.True(t, ok, "alignment must be measured against a real VictoriaMetrics, never a fake")

	samples := make([]telemetry.Sample, 0, int(span.Seconds()/10)+1)
	for offset := time.Duration(0); offset <= span; offset += 10 * time.Second {
		samples = append(samples, telemetry.Sample{
			Name:   metricAvgName,
			Value:  float64(offset / time.Second),
			TS:     start.Add(offset),
			Labels: map[string]string{metricDimLabel: "cpu.util"},
		})
	}
	require.NoError(t, client.WriteSamples(ctx, tenant, device, samples))
	require.NoError(t, client.Flush(ctx))
}

func TestMetricGridMatchesVictoriaMetricsEvaluationInstants(t *testing.T) {
	srv, m := newVMBackedServer(t)
	ctx := context.Background()
	tenant := uuid.New()
	device := uuid.New()

	dataStart := time.Now().UTC().Truncate(600 * time.Second).Add(-2 * time.Hour)
	writeRawWindow(t, srv, ctx, tenant, device, dataStart, 20*time.Minute)

	for _, tc := range []struct {
		name    string
		from    time.Time
		to      time.Time
		step    time.Duration
		buckets int
	}{
		{"aligned window, coarse step", dataStart, dataStart.Add(20 * time.Minute), 60 * time.Second, 20},
		{"unaligned start, many buckets", dataStart.Add(7 * time.Second), dataStart.Add(20 * time.Minute), 10 * time.Second, 119},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grid := buildMetricGrid(tc.from, tc.to, tc.step)
			require.Len(t, grid.ts, tc.buckets)

			series, err := srv.telemetryReader.QueryRange(ctx, tenant, telemetry.RangeQuery{
				Metric:   metricAvgName,
				Matchers: map[string]string{metricDeviceIDLabel: device.String()},
				Agg:      telemetry.RangeAvg,
				Start:    grid.queryStart(), End: grid.queryEnd(), Step: tc.step,
			})
			require.NoError(t, err)
			require.Len(t, series, 1)

			require.NotEmpty(t, series[0].Timestamps)
			for _, ts := range series[0].Timestamps {
				slot, ok := grid.slot(ts)
				require.True(t, ok, "VictoriaMetrics answered at %d, off the server grid starting %d step %d", ts, grid.ts[0], grid.step)
				require.Equal(t, grid.ts[slot], ts)
			}
			assert.Equal(t, grid.ts, series[0].Timestamps)
		})
	}

	assert.Zero(t, promtestutil.ToFloat64(m.MetricsGridMisalignedTotal))
}

func TestGetDeviceMetricsSevenDayWindowOverTwentyMinutesOfData(t *testing.T) {
	srv, m := newVMBackedServer(t)
	ctx := context.Background()
	tenant := uuid.New()
	device := uuid.New()

	to := time.Now().UTC().Truncate(time.Second)
	from := to.Add(-7 * 24 * time.Hour)
	dataStart := to.Add(-3 * 24 * time.Hour).Truncate(10 * time.Second)
	writeRawWindow(t, srv, ctx, tenant, device, dataStart, 20*time.Minute)

	step := chooseStep(from, to, defaultMaxPoints)
	resp, err := srv.buildMetricRange(ctx, tenant, device, metricRangeQuery{
		from: from, to: to, step: step, wantBand: false,
	})
	require.NoError(t, err)

	stepSecs := int64(step.Seconds())
	wantBuckets := int(int64(to.Sub(from).Seconds()) / stepSecs)
	require.Len(t, resp.T, wantBuckets, "the axis spans the requested window, not the data")
	assert.Equal(t, int(stepSecs), resp.BucketS)
	assert.True(t, resp.Downsampled)
	assert.LessOrEqual(t, resp.T[0], from.Unix())
	assert.Greater(t, resp.T[0], from.Unix()-stepSecs)
	assert.Equal(t, resp.T[0]+int64(wantBuckets-1)*stepSecs, resp.T[len(resp.T)-1])

	require.Len(t, resp.Series, 1)
	vals := resp.Series[0].Avg
	require.Len(t, vals, wantBuckets)

	filled := nonNullSlots(vals)
	require.NotEmpty(t, filled, "the 20 minutes of data must render")
	assert.Less(t, len(filled), 10)
	assert.Equal(t, filled[len(filled)-1]-filled[0]+1, len(filled), "the run is contiguous")
	for _, slot := range filled {
		assert.GreaterOrEqual(t, resp.T[slot], dataStart.Unix()-stepSecs)
		assert.LessOrEqual(t, resp.T[slot], dataStart.Add(20*time.Minute).Unix()+stepSecs)
	}
	assert.Zero(t, promtestutil.ToFloat64(m.MetricsGridMisalignedTotal),
		"a query issued on the grid's own instants can never answer off it")
}

func TestGetDeviceMetricsWindowSelectorChangesThePointCount(t *testing.T) {
	srv, _ := newVMBackedServer(t)
	ctx := context.Background()
	tenant := uuid.New()
	device := uuid.New()

	to := time.Now().UTC().Truncate(time.Second)
	writeRawWindow(t, srv, ctx, tenant, device, to.Add(-20*time.Minute).Truncate(10*time.Second), 20*time.Minute)

	counts := make(map[time.Duration]int)
	for _, window := range []time.Duration{time.Hour, 6 * time.Hour, 24 * time.Hour, 7 * 24 * time.Hour} {
		from := to.Add(-window)
		step := chooseStep(from, to, defaultMaxPoints)
		resp, err := srv.buildMetricRange(ctx, tenant, device, metricRangeQuery{from: from, to: to, step: step})
		require.NoError(t, err)
		counts[window] = len(resp.T)
		assert.LessOrEqual(t, len(resp.T), maxMaxPointsBound)
	}

	assert.Equal(t, 60, counts[time.Hour])
	assert.Equal(t, 360, counts[6*time.Hour])
	assert.Equal(t, 993, counts[24*time.Hour])
	assert.Equal(t, 999, counts[7*24*time.Hour])
	assert.Len(t, counts, 4, "every preset renders its own axis — the selector is not a dead control")
}
