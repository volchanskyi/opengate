package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// A rule version that narrows a range moves the customer's value, records that
// it did, and keeps saying so until an administrator acknowledges it.
func TestAClampIsRecordedOnceAndSurfacedUntilAcknowledged(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	tuned := orgBinding(e.org, "disk-critical", threshold(95))
	require.NoError(t, s.UpsertBinding(e.ctx, cat, tuned))

	narrowed := catalogueWith(t, narrowedDisk(t, Bounds{Min: 50, Max: 90}))

	outstanding, err := s.ReconcileClamps(e.ctx, narrowed, e.org)
	require.NoError(t, err)
	require.Len(t, outstanding, 1)
	assert.Equal(t, tuned.ID, outstanding[0].BindingID)
	assert.Equal(t, "threshold", outstanding[0].Param)
	assert.InEpsilon(t, 95.0, outstanding[0].From, 0.0001)
	assert.InEpsilon(t, 90.0, outstanding[0].To, 0.0001)
	assert.True(t, outstanding[0].Outstanding())

	// Reading the same upgrade again records the same single move rather than
	// one per read.
	again, err := s.ReconcileClamps(e.ctx, narrowed, e.org)
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Equal(t, outstanding[0].ID, again[0].ID)

	require.NoError(t, s.AcknowledgeClamp(e.ctx, outstanding[0].ID, "ivan"))
	assert.Empty(t, mustListClamps(t, s, e.ctx, e.org),
		"an acknowledged move stops being a flag")

	// Acknowledging is not a repeatable action: the second attempt has nothing
	// outstanding to act on.
	require.ErrorIs(t, s.AcknowledgeClamp(e.ctx, outstanding[0].ID, "ivan"), ErrClampNotFound)
}

// Tuning the rule version still allows records nothing. A flag raised on every
// upgrade is a flag nobody reads.
func TestAnUpgradeThatStillAllowsTheValueRecordsNothing(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	require.NoError(t, s.UpsertBinding(e.ctx, cat, orgBinding(e.org, "disk-critical", threshold(85))))

	outstanding, err := s.ReconcileClamps(e.ctx, catalogueWith(t, narrowedDisk(t, Bounds{Min: 50, Max: 90})), e.org)
	require.NoError(t, err)
	assert.Empty(t, outstanding)
}

// A move belongs to the customer whose tuning was moved. Two customers inside
// one tenant is what proves it: nothing in the database keeps them apart.
func TestAClampIsScopedToOneCustomer(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)
	narrowed := catalogueWith(t, narrowedDisk(t, Bounds{Min: 50, Max: 90}))

	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	require.NoError(t, s.UpsertBinding(e.ctx, cat, orgBinding(e.org, "disk-critical", threshold(95))))
	require.NoError(t, s.UpsertBinding(e.ctx, cat, orgBinding(other, "disk-critical", threshold(99))))

	mine, err := s.ReconcileClamps(e.ctx, narrowed, e.org)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, e.org, mine[0].OrganizationID)
	assert.Empty(t, mustListClamps(t, s, e.ctx, other),
		"reconciling one customer must not record another's")
}

// Deleting the binding takes its moves with it: a flag pointing at tuning that
// no longer exists cannot be acted on.
func TestDeletingATunedBindingClearsItsClamps(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	tuned := orgBinding(e.org, "disk-critical", threshold(95))
	require.NoError(t, s.UpsertBinding(e.ctx, cat, tuned))
	_, err := s.ReconcileClamps(e.ctx, catalogueWith(t, narrowedDisk(t, Bounds{Min: 50, Max: 90})), e.org)
	require.NoError(t, err)
	require.Len(t, mustListClamps(t, s, e.ctx, e.org), 1)

	require.NoError(t, s.DeleteBinding(e.ctx, tuned.ID))
	assert.Empty(t, mustListClamps(t, s, e.ctx, e.org))
}

// A read with no tenant on the context is refused rather than answered.
func TestClampsRequireTenantScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bare := context.Background()

	_, err := s.ListClamps(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	assert.ErrorIs(t, s.AcknowledgeClamp(bare, uuid.New(), "ivan"), dbtx.ErrTenantRequired)
}

// mustListClamps reads one customer's outstanding moves.
func mustListClamps(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Clamp {
	t.Helper()
	got, err := s.ListClamps(ctx, org)
	require.NoError(t, err)
	return got
}
