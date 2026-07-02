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

func TestVMClientWritesOrgScopedSamples(t *testing.T) {
	base := testvm.BaseURL(t)
	client := NewVMClient(base, nil)
	ctx := context.Background()

	orgA := uuid.New()
	orgB := uuid.New()
	deviceID := uuid.New()
	ts := time.Now().UTC().Truncate(time.Second)

	require.NoError(t, client.WriteSamples(ctx, orgA, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  41,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.WriteSamples(ctx, orgB, deviceID, []Sample{{
		Name:   "opengate_test_ws4_metric",
		Value:  82,
		TS:     ts,
		Labels: map[string]string{"dim": "cpu"},
	}}))
	require.NoError(t, client.Flush(ctx))

	series, err := client.Export(ctx, orgA, `opengate_test_ws4_metric{device_id="`+deviceID.String()+`"}`, ts.Add(-time.Minute), ts.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, series, 1)
	assert.Equal(t, orgA.String(), series[0].Metric["org_id"])
	assert.Equal(t, []float64{41}, series[0].Values)
}

func TestScopeSelectorRejectsCallerSuppliedOrgID(t *testing.T) {
	orgID := uuid.New()

	_, err := ScopeSelector(`opengate_test_ws4_metric{org_id="other"}`, orgID)

	assert.ErrorIs(t, err, ErrOrgMatcherNotAllowed)
}

func TestScopeSelectorInjectsOrgID(t *testing.T) {
	orgID := uuid.New()

	got, err := ScopeSelector(`opengate_test_ws4_metric{device_id="dev-1"}`, orgID)

	require.NoError(t, err)
	assert.Equal(t, `opengate_test_ws4_metric{org_id="`+orgID.String()+`",device_id="dev-1"}`, got)
}
