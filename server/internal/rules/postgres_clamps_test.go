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

func TestAClampIsRecordedOnceAndSurfacedUntilAcknowledged(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	tuned := tuneDisk(t, s, e, e.org, 95)

	outstanding, err := reconcileNarrowed(t, s, e, e.org)
	require.NoError(t, err)
	require.Len(t, outstanding, 1)
	assert.Equal(t, tuned.ID, outstanding[0].BindingID)
	assert.Equal(t, "threshold", outstanding[0].Param)
	assert.InEpsilon(t, 95.0, outstanding[0].From, 0.0001)
	assert.InEpsilon(t, 90.0, outstanding[0].To, 0.0001)
	assert.True(t, outstanding[0].Outstanding())

	again, err := reconcileNarrowed(t, s, e, e.org)
	require.NoError(t, err)
	require.Len(t, again, 1)
	assert.Equal(t, outstanding[0].ID, again[0].ID)

	require.NoError(t, s.AcknowledgeClamp(e.ctx, outstanding[0].ID, "ivan"))
	assert.Empty(t, mustListClamps(t, s, e.ctx, e.org),
		"an acknowledged move stops being a flag")

	require.ErrorIs(t, s.AcknowledgeClamp(e.ctx, outstanding[0].ID, "ivan"), ErrClampNotFound)
}

func TestAnUpgradeThatStillAllowsTheValueRecordsNothing(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	tuneDisk(t, s, e, e.org, 85)

	outstanding, err := reconcileNarrowed(t, s, e, e.org)
	require.NoError(t, err)
	assert.Empty(t, outstanding)
}

func TestAClampIsScopedToOneCustomer(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	tuneDisk(t, s, e, e.org, 95)
	tuneDisk(t, s, e, other, 99)

	mine, err := reconcileNarrowed(t, s, e, e.org)
	require.NoError(t, err)
	require.Len(t, mine, 1)
	assert.Equal(t, e.org, mine[0].OrganizationID)
	assert.Empty(t, mustListClamps(t, s, e.ctx, other),
		"reconciling one customer must not record another's")
}

func TestDeletingATunedBindingClearsItsClamps(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	tuned := tuneDisk(t, s, e, e.org, 95)
	_, err := reconcileNarrowed(t, s, e, e.org)
	require.NoError(t, err)
	require.Len(t, mustListClamps(t, s, e.ctx, e.org), 1)

	require.NoError(t, s.DeleteBinding(e.ctx, tuned.ID))
	assert.Empty(t, mustListClamps(t, s, e.ctx, e.org))
}

func TestClampsRequireTenantScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bare := context.Background()

	_, err := s.ListClamps(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	assert.ErrorIs(t, s.AcknowledgeClamp(bare, uuid.New(), "ivan"), dbtx.ErrTenantRequired)
}

func reconcileNarrowed(t *testing.T, s *Store, e estate, org uuid.UUID) ([]Clamp, error) {
	t.Helper()
	return s.ReconcileClamps(e.ctx, catalogueWith(t, narrowedDisk(t, Bounds{Min: 50, Max: 90})), org)
}

func tuneDisk(t *testing.T, s *Store, e estate, org uuid.UUID, value float64) Binding {
	t.Helper()
	tuned := orgBinding(org, "disk-critical", threshold(value))
	require.NoError(t, s.UpsertBinding(e.ctx, mustCatalogue(t), tuned))
	return tuned
}

func mustListClamps(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Clamp {
	t.Helper()
	got, err := s.ListClamps(ctx, org)
	require.NoError(t, err)
	return got
}
