package rules

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestFleetCoverageCountsEveryTenantsMachines(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	neighbourID := uuid.New()
	admin := dbtx.WithDefaultTenant(context.Background(), true)
	testutil.EnsureTenant(t, admin, e.store, neighbourID, "Neighbour "+neighbourID.String()[:8])
	neighbour := dbtx.WithTenant(context.Background(), neighbourID, false)
	site := testutil.SeedSite(t, neighbour, e.store)
	machine := testutil.SeedDevice(t, neighbour, e.store, site.ID)
	require.NoError(t, s.MarkUnsupported(neighbour, site.OrganizationID, machine.ID, "io-stalled"))
	require.NoError(t, s.MarkUnsupported(neighbour, site.OrganizationID, machine.ID, "disk-slow"))

	fleet, blind, err := s.FleetCoverage(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, fleet, "both tenants' machines make up the fleet")
	assert.Equal(t, map[string]int{"io-stalled": 2, "disk-slow": 1}, blind,
		"a standing hole is counted wherever it is, in whichever tenant")
}

func TestFleetCoverageNeedsNoCallerScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	_, ok := dbtx.TenantFromContext(context.Background())
	require.False(t, ok, "the case is only meaningful on an unscoped context")

	fleet, blind, err := s.FleetCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fleet)
	assert.Equal(t, map[string]int{"io-stalled": 1}, blind)
}

func TestFleetCoverageAnswersAnEmptyEstate(t *testing.T) {
	t.Parallel()

	s, _ := newEstate(t)

	fleet, blind, err := s.FleetCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fleet, "the estate's one machine is still the fleet")
	assert.Empty(t, blind, "nothing is blind to anything yet")
}

func TestFleetCoverageSurfacesAReadThatCannotBeAnswered(t *testing.T) {
	t.Parallel()

	blindStore := NewStore(testutil.NewUnmigratedDB(t))

	fleet, blind, err := blindStore.FleetCoverage(context.Background())
	require.Error(t, err, "a read that cannot reach the tables is a failure, not an empty install")
	assert.Contains(t, err.Error(), "count fleet coverage")
	assert.Zero(t, fleet)
	assert.Nil(t, blind)
}

func TestFleetCoverageIsOneStatement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, strings.Count(fleetCoverageSQL, "GROUP BY"),
		"the split is the database's work")
	assert.NotContains(t, fleetCoverageSQL, scopedToTenant,
		"the platform's own view is of every tenant, so there is nothing for a predicate to confine it to")
}
