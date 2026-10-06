package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestStoreUnsupportedCoverageSurvivesAndOnlyEverStoresUnsupported(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	blind := map[string]int{"io-stalled": 1}
	assert.Equal(t, blind, mustCountUnsupported(t, s, e.ctx, e.org))

	fresh := NewStore(e.store.DB())
	assert.Equal(t, blind, mustCountUnsupported(t, fresh, e.ctx, e.org))

	since := mustUnsupportedSince(t, s, e.ctx, e.device, "io-stalled")
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))
	assert.Equal(t, since, mustUnsupportedSince(t, s, e.ctx, e.device, "io-stalled"),
		"a repeated report must not reset when the hole opened")

	require.NoError(t, s.ClearUnsupported(e.ctx, e.device, "io-stalled"))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org))
}

func TestStoreCoverageIsErasedWithItsDevice(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	require.NoError(t, s.EraseDeviceCoverage(e.ctx, e.device))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org))

	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))
	require.NoError(t, testutil.NewTestDevices(t, e.store).Delete(e.ctx, e.device))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org),
		"a deleted machine must not still be counted")
}
