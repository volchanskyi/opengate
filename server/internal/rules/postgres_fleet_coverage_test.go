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

// The fleet-wide half of coverage: what the platform's own monitoring reads.
//
// Per-customer coverage answers "how much of Contoso's estate is this rule
// watching". This answers "how much of everything is it watching", which is the
// question a staged rollout is actually being judged on — and it has to be
// answerable about tenants this process is not currently serving requests for,
// because a rule that reached nobody in a quiet tenant reached nobody.

// TestFleetCoverageCountsEveryTenantsMachines is the whole claim: one fleet size
// and one blind-spot count per rule, summed across every tenant.
func TestFleetCoverageCountsEveryTenantsMachines(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	require.NoError(t, s.MarkUnsupported(e.ctx, e.org, e.device, "io-stalled"))

	// A second tenant with its own machine, blind to the same rule.
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

// TestFleetCoverageNeedsNoCallerScope states the contract the metrics updater
// relies on: it runs on a background goroutine belonging to no request, so the
// read scopes itself.
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

// TestFleetCoverageAnswersAnEmptyEstate keeps the read from failing on the state
// every install starts in. Nothing blind is an empty map, not an error and not a
// missing fleet size.
func TestFleetCoverageAnswersAnEmptyEstate(t *testing.T) {
	t.Parallel()

	s, _ := newEstate(t)

	fleet, blind, err := s.FleetCoverage(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, fleet, "the estate's one machine is still the fleet")
	assert.Empty(t, blind, "nothing is blind to anything yet")
}

// TestFleetCoverageSurfacesAReadThatCannotBeAnswered is the negative case. A
// fleet size of zero is a meaningful answer — an install with no machines — so
// a read that could not be completed must not return one. The caller would
// otherwise publish "watching nothing" when what happened is that it failed to
// look.
func TestFleetCoverageSurfacesAReadThatCannotBeAnswered(t *testing.T) {
	t.Parallel()

	blindStore := NewStore(testutil.NewUnmigratedDB(t))

	fleet, blind, err := blindStore.FleetCoverage(context.Background())
	require.Error(t, err, "a read that cannot reach the tables is a failure, not an empty install")
	assert.Contains(t, err.Error(), "count fleet coverage")
	assert.Zero(t, fleet)
	assert.Nil(t, blind)
}

// TestFleetCoverageIsOneStatement is the bound. The caller refreshes a gauge
// from this on a timer, so counting the fleet and counting the blind spots is
// one aggregate rather than a query each — and never one per rule.
func TestFleetCoverageIsOneStatement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, strings.Count(fleetCoverageSQL, "GROUP BY"),
		"the split is the database's work")
	assert.NotContains(t, fleetCoverageSQL, scopedToTenant,
		"the platform's own view is of every tenant, so there is nothing for a predicate to confine it to")
}
