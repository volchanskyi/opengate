package telemetry

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testvm"
)

func TestBackfillImportLandsInHistoricalBuckets(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	tenant := uuid.New()
	device := uuid.New()
	now := time.Now().UTC().Truncate(time.Hour)

	backfilled := []struct {
		age time.Duration
		val float64
	}{
		{72 * time.Hour, 11},
		{48 * time.Hour, 22},
		{8 * time.Hour, 33},
	}
	for _, b := range backfilled {
		require.NoError(t, client.WriteSamples(ctx, tenant, device, []Sample{{
			Name: "opengate_edge_metric_avg", Value: b.val, TS: now.Add(-b.age),
			Labels: map[string]string{"dim": "cpu.total"},
		}}))
	}
	require.NoError(t, client.Flush(ctx))

	series, err := client.QueryRange(ctx, tenant, RangeQuery{
		Metric:   "opengate_edge_metric_avg",
		Matchers: map[string]string{"device_id": device.String(), "dim": "cpu.total"},
		Agg:      RangeAvg,
		Start:    now.Add(-96 * time.Hour),
		End:      now,
		Step:     time.Hour,
	})
	require.NoError(t, err)
	require.Len(t, series, 1)
	require.Len(t, series[0].Timestamps, len(series[0].Values))
	require.NotEmpty(t, series[0].Values)

	for _, b := range backfilled {
		wantTS := now.Add(-b.age).Unix()
		found := false
		for i, ts := range series[0].Timestamps {
			if math.Abs(series[0].Values[i]-b.val) < 1e-6 && abs64(ts-wantTS) <= int64(time.Hour.Seconds()) {
				found = true
				break
			}
		}
		require.Truef(t, found, "backfilled value %v missing near its historical bucket %d; got=%v/%v",
			b.val, wantTS, series[0].Timestamps, series[0].Values)
	}

	arrivalFloor := now.Add(-6 * time.Hour).Unix()
	for _, ts := range series[0].Timestamps {
		assert.Lessf(t, ts, arrivalFloor,
			"sample at %d collapsed toward ingest time (%d) — the stream-agg bucketing trap", ts, arrivalFloor)
	}
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
