package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVMClientCountAnomalyBands(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenantA := uuid.New()
	tenantB := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	at := ts.Add(time.Minute)

	for _, rate := range []float64{0.42, 0.31, 0.15, 0.09, 0.0, 0.02} {
		writeAnomalyRate(t, client, ctx, tenantA, uuid.New(), rate, ts)
	}
	writeAnomalyRate(t, client, ctx, tenantB, uuid.New(), 0.99, ts)

	bands, err := client.CountAnomalyBands(ctx, tenantA, 0.1, 0.3, at, 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Anomalous: 2, Watch: 1, Healthy: 3}, bands)
}

func TestVMClientCountAnomalyBandsRouteToTheirOwnField(t *testing.T) {
	client, ctx := newTestVMClient(t)
	ts := time.Now().UTC().Truncate(time.Second)

	for _, tc := range []struct {
		name string
		rate float64
		want BandCounts
	}{
		{"anomalous only", 0.90, BandCounts{Anomalous: 1}},
		{"watch only", 0.20, BandCounts{Watch: 1}},
		{"healthy only", 0.01, BandCounts{Healthy: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := uuid.New()
			writeAnomalyRate(t, client, ctx, tenant, uuid.New(), tc.rate, ts)

			bands, err := client.CountAnomalyBands(ctx, tenant, 0.1, 0.3, ts.Add(time.Minute), 10*time.Minute)
			require.NoError(t, err)
			assert.Equal(t, tc.want, bands)
		})
	}
}

func TestVMClientCountAnomalyBandsBoundariesAreHalfOpen(t *testing.T) {
	client, ctx := newTestVMClient(t)
	tenant := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	for _, rate := range []float64{0.0, 0.1, 0.3, 1.0} {
		writeAnomalyRate(t, client, ctx, tenant, uuid.New(), rate, ts)
	}

	bands, err := client.CountAnomalyBands(ctx, tenant, 0.1, 0.3, ts.Add(time.Minute), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Anomalous: 2, Watch: 1, Healthy: 1}, bands,
		"0.3 and 1.0 are anomalous, 0.1 is watch, 0.0 is healthy")
	assert.Equal(t, 4, bands.Anomalous+bands.Watch+bands.Healthy,
		"the bands partition the range — every device is counted exactly once")
}

func TestVMClientCountAnomalyBandsEmptyBandsAreZero(t *testing.T) {
	client, ctx := newTestVMClient(t)

	tenant := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	writeAnomalyRate(t, client, ctx, tenant, uuid.New(), 0.55, ts)

	bands, err := client.CountAnomalyBands(ctx, tenant, 0.1, 0.3, ts.Add(time.Minute), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Anomalous: 1, Watch: 0, Healthy: 0}, bands)
}

func TestVMClientCountAnomalyBandsNoDevices(t *testing.T) {
	client, ctx := newTestVMClient(t)

	bands, err := client.CountAnomalyBands(ctx, uuid.New(), 0.1, 0.3, time.Now().UTC(), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{}, bands)
}

func TestVMClientCountAnomalyBandsRejectsNilTenant(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	_, err := client.CountAnomalyBands(context.Background(), uuid.Nil, 0.1, 0.3, time.Now(), time.Minute)
	require.Error(t, err, "an unscoped band count must be refused")
}
