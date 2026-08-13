package rules

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The durable third of coverage, against a real database: which machines cannot
// evaluate which rules, and what survives a restart, an offline machine, and a
// decommissioning.

// The durable third of coverage. A machine that cannot evaluate a rule stays
// counted while it is offline and across a restart, because that is a standing
// fact about the estate rather than a liveness reading.
func TestStoreUnsupportedCoverageSurvivesAndOnlyEverStoresUnsupported(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	blind := map[string]int{"io-stalled": 1}
	assert.Equal(t, blind, mustCountUnsupported(t, s, e.ctx, e.org))

	// Reading it again — the server having restarted changes nothing, because
	// nothing about this lives in memory.
	fresh := NewStore(e.store.DB())
	assert.Equal(t, blind, mustCountUnsupported(t, fresh, e.ctx, e.org))

	// Marking it again is idempotent and keeps the original since, which is what
	// makes "blind since March" answerable.
	since := mustUnsupportedSince(t, s, e.ctx, e.device, "io-stalled")
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))
	assert.Equal(t, since, mustUnsupportedSince(t, s, e.ctx, e.device, "io-stalled"),
		"a repeated report must not reset when the hole opened")

	// A machine that starts evaluating the rule loses its row rather than
	// storing an 'active' state that could later go stale.
	require.NoError(t, s.ClearUnsupported(e.ctx, e.device, "io-stalled"))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org))
}

// A decommissioned machine must not inflate the unsupported count forever.
// A decommissioned machine must not inflate the unsupported count forever.
func TestStoreCoverageIsErasedWithItsDevice(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	require.NoError(t, s.EraseDeviceCoverage(e.ctx, e.device))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org))

	// Deleting the machine itself takes any rows with it too, so the erase is
	// belt and braces rather than the only thing standing between the count and
	// a lie.
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))
	require.NoError(t, testutil.NewTestDevices(t, e.store).Delete(e.ctx, e.device))
	assert.Empty(t, mustCountUnsupported(t, s, e.ctx, e.org),
		"a deleted machine must not still be counted")
}

// Every table this package writes is behind forced row-level security, so
// another tenant cannot read or write these rows even by asking directly.
