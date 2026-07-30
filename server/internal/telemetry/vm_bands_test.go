package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVMClientCountAnomalyBands proves the dashboard's O(1) health rollup: one
// instant query returns a per-band device count, scoped to one organization,
// with no per-device rows crossing the boundary.
func TestVMClientCountAnomalyBands(t *testing.T) {
	client, ctx := newTestVMClient(t)

	orgA := uuid.New()
	orgB := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	at := ts.Add(time.Minute)

	// orgA: two anomalous (>= 0.3), one watch ([0.1, 0.3)), three healthy (< 0.1).
	for _, rate := range []float64{0.42, 0.31, 0.15, 0.09, 0.0, 0.02} {
		writeAnomalyRate(t, client, ctx, orgA, uuid.New(), rate, ts)
	}
	// orgB must never leak into orgA's counts.
	writeAnomalyRate(t, client, ctx, orgB, uuid.New(), 0.99, ts)

	bands, err := client.CountAnomalyBands(ctx, orgA, 0.1, 0.3, at, 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Anomalous: 2, Watch: 1, Healthy: 3}, bands)
}

// TestVMClientCountAnomalyBandsEmptyBandsAreZero pins the sharp edge of the
// PromQL: count() over an empty set returns no sample at all, not zero. A band
// with no devices must still come back as 0 rather than a missing value.
func TestVMClientCountAnomalyBandsEmptyBandsAreZero(t *testing.T) {
	client, ctx := newTestVMClient(t)

	org := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)
	writeAnomalyRate(t, client, ctx, org, uuid.New(), 0.55, ts)

	bands, err := client.CountAnomalyBands(ctx, org, 0.1, 0.3, ts.Add(time.Minute), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{Anomalous: 1, Watch: 0, Healthy: 0}, bands)
}

// TestVMClientCountAnomalyBandsNoDevices covers the empty organization: every
// band is absent from the result and every count reads zero.
func TestVMClientCountAnomalyBandsNoDevices(t *testing.T) {
	client, ctx := newTestVMClient(t)

	bands, err := client.CountAnomalyBands(ctx, uuid.New(), 0.1, 0.3, time.Now().UTC(), 10*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, BandCounts{}, bands)
}

func TestVMClientCountAnomalyBandsRejectsNilOrg(t *testing.T) {
	t.Parallel()
	client := NewVMClient("http://127.0.0.1:0", nil)
	_, err := client.CountAnomalyBands(context.Background(), uuid.Nil, 0.1, 0.3, time.Now(), time.Minute)
	require.Error(t, err, "an unscoped band count must be refused")
}
