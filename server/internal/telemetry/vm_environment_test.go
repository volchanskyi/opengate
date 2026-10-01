package telemetry

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the server writes names the environment it runs in. Production and
// staging write into one store, and a dashboard asked about one of them read
// the machine readings, anomaly rates and breaches of both, added together.

func TestVMClientStampsWhatItWritesWithItsEnvironment(t *testing.T) {
	client, ctx := newTestVMClient(t)
	stamped := client.WithNamespace("opengate-staging")

	tenant, device := uuid.New(), uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, stamped.WriteSamples(ctx, tenant, device, []Sample{{
		Name: "opengate_test_env_metric", Value: 1, TS: ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, tenant, `opengate_test_env_metric{device_id="`+device.String()+`"}`,
		ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, "opengate-staging", series[0].Metric["namespace"])
}

// The environment is the server's to state, never a sample's.
func TestASampleCannotNameItsOwnEnvironment(t *testing.T) {
	client, ctx := newTestVMClient(t)
	err := client.WithNamespace("opengate").WriteSamples(ctx, uuid.New(), uuid.New(), []Sample{{
		Name: "opengate_test_env_metric", Value: 1,
		Labels: map[string]string{"namespace": "opengate-staging"},
	}})
	require.ErrorIs(t, err, ErrReservedLabel)
}

// A device lives in one environment, so the stamp is metadata for dashboards
// and not part of what a reading is. Readings written before the stamp and
// readings written after it are one device's one series to every read the
// product makes — the chart, the health badge and the fleet bands. Without
// that, every chart would draw each measurement twice, split at the deploy,
// for the thirty days the older readings stay in the store, and the fleet
// bands would count a device twice while both sat inside their lookback.
func TestReadingsFromBeforeAndAfterTheEnvironmentStampAreOneSeries(t *testing.T) {
	client, ctx := newTestVMClient(t)
	stamped := client.WithNamespace("opengate")

	tenant, device := uuid.New(), uuid.New()
	end := time.Now().UTC().Truncate(10 * time.Second)
	start := end.Add(-10 * time.Minute)
	for i := 0; i <= 60; i++ {
		ts := start.Add(time.Duration(i) * 10 * time.Second)
		writer := client
		if i > 30 {
			writer = stamped
		}
		require.NoError(t, writer.WriteSamples(ctx, tenant, device, []Sample{
			{Name: "opengate_edge_metric_avg", Value: 50, TS: ts, Labels: map[string]string{"dim": "cpu.util"}},
			{Name: MetricNodeAnomalyRate, Value: 0.5, TS: ts},
		}))
	}
	require.NoError(t, client.Flush(ctx))

	for _, agg := range []RangeAgg{RangeAvg, RangeMin, RangeMax} {
		series, err := client.QueryRange(ctx, tenant, RangeQuery{
			Metric: "opengate_edge_metric_avg", Matchers: map[string]string{"device_id": device.String()},
			Agg: agg, Start: start, End: end, Step: time.Minute,
		})
		require.NoError(t, err)
		require.Lenf(t, series, 1, "the %s chart line is one series across the stamp", agg)
		assert.GreaterOrEqual(t, len(series[0].Values), 10, "and it covers both halves of the window")
	}

	badge, err := client.QueryInstantLookback(ctx, tenant, MetricNodeAnomalyRate,
		map[string]string{"device_id": device.String()}, end, 15*time.Minute)
	require.NoError(t, err)
	require.Len(t, badge, 1, "the health badge reads one value for the device")

	bands, err := client.CountAnomalyBands(ctx, tenant, 0.3, 0.8, end, 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Watch: 1}, bands, "and the fleet bands count it once")
}
