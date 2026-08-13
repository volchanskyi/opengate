package rules

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// estate is one customer's seeded rows: the customer, a site, and a machine in
// it, which is the least a binding or a coverage row needs to point at.
type estate struct {
	store  *db.PostgresStore
	ctx    context.Context
	org    uuid.UUID
	site   uuid.UUID
	device uuid.UUID
}

func newEstate(t *testing.T) (*Store, estate) {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	device := testutil.SeedDevice(t, ctx, store, site.ID)
	return NewStore(store.DB()), estate{
		store:  store,
		ctx:    ctx,
		org:    site.OrganizationID,
		site:   site.ID,
		device: device.ID,
	}
}

// mustUnsupportedSince reads when a machine first reported it could not
// evaluate a rule, failing if it has no such row at all.
func mustUnsupportedSince(t *testing.T, s *Store, ctx context.Context, device uuid.UUID, ruleID string) time.Time {
	t.Helper()
	since, ok, err := s.UnsupportedSince(ctx, device, ruleID)
	require.NoError(t, err)
	require.True(t, ok, "%s must have an unsupported row for %s", device, ruleID)
	return since
}

func TestStoreBindingRoundTrips(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat, err := Embedded()
	require.NoError(t, err)

	want := targeted(
		newBinding(e.org, "disk-critical", settings.LevelSite, e.site,
			map[string]float64{"threshold": 95, "sustain_secs": 600}),
		Selector{"role": "file-server"}, 10)
	want.UpdatedBy = "ivan"
	require.NoError(t, s.UpsertBinding(e.ctx, cat, want))

	got := mustListBindings(t, s, e.ctx, e.org)
	require.Len(t, got, 1)
	assert.Equal(t, want.ID, got[0].ID)
	assert.Equal(t, settings.LevelSite, got[0].Level)
	assert.Equal(t, e.site, got[0].LevelKey)
	assert.Equal(t, Selector{"role": "file-server"}, got[0].Selector)
	assert.Equal(t, 10, got[0].Precedence)
	assert.InEpsilon(t, 95.0, got[0].Params["threshold"], 0.0001)
	assert.InEpsilon(t, 600.0, got[0].Params["sustain_secs"], 0.0001)

	// The same key retunes rather than duplicating.
	want.Params = threshold(92)
	require.NoError(t, s.UpsertBinding(e.ctx, cat, want))
	got = mustListBindings(t, s, e.ctx, e.org)
	require.Len(t, got, 1)
	assert.InEpsilon(t, 92.0, got[0].Params["threshold"], 0.0001)

	require.NoError(t, s.DeleteBinding(e.ctx, want.ID))
	assert.Empty(t, mustListBindings(t, s, e.ctx, e.org))
}

// Validation happens on write, so a value outside the rule's bounds never
// reaches a row at all.
func TestStoreRefusesABindingOutsideTheRulesBounds(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat, err := Embedded()
	require.NoError(t, err)

	err = s.UpsertBinding(e.ctx, cat, orgBinding(e.org, "disk-critical", threshold(100)))
	require.ErrorIs(t, err, ErrParamOutOfBounds)

	err = s.UpsertBinding(e.ctx, cat, orgBinding(e.org, "no-such-rule", nil))
	require.ErrorIs(t, err, ErrUnknownRule)

	assert.Empty(t, mustListBindings(t, s, e.ctx, e.org),
		"a refused binding must leave nothing behind")
}

// The database refuses the ambiguity resolution would otherwise have to guess
// its way out of: two selectors at one rung with one precedence.
func TestStoreRefusesTwoSelectorsAtOneRungWithOnePrecedence(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat, err := Embedded()
	require.NoError(t, err)

	at := func(selector Selector, precedence int) Binding {
		return targeted(orgBinding(e.org, "disk-critical", threshold(95)), selector, precedence)
	}

	require.NoError(t, s.UpsertBinding(e.ctx, cat, at(Selector{"role": "file-server"}, 10)))
	require.Error(t, s.UpsertBinding(e.ctx, cat, at(Selector{"env": "prod"}, 10)),
		"a second selector at the same rung and precedence must be refused")

	// A different precedence states which one wins, so it is allowed.
	require.NoError(t, s.UpsertBinding(e.ctx, cat, at(Selector{"env": "prod"}, 20)))

	// The rung's blanket binding is unaffected: it is already ordered behind
	// every targeted one, so it needs no precedence of its own.
	require.NoError(t, s.UpsertBinding(e.ctx, cat, at(nil, 10)))
}

// Every table this package writes is behind forced row-level security, so
// another tenant cannot read or write these rows even by asking directly.
func TestStoreDeniesCrossTenantAccess(t *testing.T) {
	t.Parallel()

	store := testutil.NewTestStore(t)
	cat, err := Embedded()
	require.NoError(t, err)

	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	siteA := testutil.SeedSite(t, ctxA, store)
	deviceA := testutil.SeedDevice(t, ctxA, store, siteA.ID)
	siteB := testutil.SeedSite(t, ctxB, store)

	s := NewStore(store.DB())

	require.NoError(t, s.UpsertBinding(ctxA, cat,
		orgBinding(siteA.OrganizationID, "disk-critical", threshold(95))))
	require.NoError(t, s.UpsertRollout(ctxA, DefaultRollout(siteA.OrganizationID, "disk-critical")))
	require.NoError(t, s.MarkUnsupported(ctxA, siteA.OrganizationID, deviceA.ID, "io-stalled"))

	// Tenant B cannot see any of it, even naming tenant A's customer.
	assert.Empty(t, mustListBindings(t, s, ctxB, siteA.OrganizationID))
	assert.Empty(t, mustListRollouts(t, s, ctxB, siteA.OrganizationID))
	assert.Empty(t, mustCountUnsupported(t, s, ctxB, siteA.OrganizationID))

	// Nor can it write into tenant A's customer: the row check refuses it.
	err = s.UpsertBinding(ctxB, cat,
		orgBinding(siteA.OrganizationID, "cpu-saturated", threshold(60)))
	require.Error(t, err, "a write into another tenant's customer must be refused")

	// Tenant A still sees exactly its own row, and B's own estate is untouched.
	assert.Len(t, mustListBindings(t, s, ctxA, siteA.OrganizationID), 1)
	assert.Empty(t, mustListBindings(t, s, ctxB, siteB.OrganizationID))
}

// A read with no tenant on the context is refused rather than answered.
func TestStoreRequiresTenantScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bare := context.Background()

	_, err := s.ListBindings(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = s.ListRollouts(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = s.CountUnsupported(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	assert.ErrorIs(t, s.MarkUnsupported(bare, e.org, e.device, "io-stalled"), dbtx.ErrTenantRequired)
}
