package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// The label list belongs to one customer, and a label is assignable only to that
// customer's machines. Two customers inside one tenant is the case that proves
// it: the isolation wall is at the tenant, so nothing in the database stops a
// label crossing between them — the scoping is the query's own job.
func TestLabelsBelongToOneCustomer(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := newLabel(e.org, "role", "file-server")
	require.NoError(t, s.CreateLabel(e.ctx, fileServer))

	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	otherSite := testutil.SeedSiteIn(t, e.ctx, e.store, other)
	otherDevice := testutil.SeedDeviceIn(t, e.ctx, e.store, other, otherSite.ID)

	assert.Equal(t, []Label{fileServer}, mustListLabels(t, s, e.ctx, e.org))
	assert.Empty(t, mustListLabels(t, s, e.ctx, other),
		"one customer's label list must not appear in another's")

	// The machine it was made for takes it.
	require.NoError(t, s.AssignTag(e.ctx, e.device, fileServer.ID, "ivan"))
	assert.Equal(t, map[string]string{"role": "file-server"}, mustTagsFor(t, s, e.ctx, e.device))

	// Another customer's machine cannot.
	err := s.AssignTag(e.ctx, otherDevice.ID, fileServer.ID, "ivan")
	require.ErrorIs(t, err, ErrLabelForeign)
	assert.Empty(t, mustTagsFor(t, s, e.ctx, otherDevice.ID))
}

// One value per key per machine. A machine answering `file-server` and
// `workstation` to the same question would make every selector that asks it
// match twice.
func TestAssigningASecondValueForOneKeyReplacesTheFirst(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := newLabel(e.org, "role", "file-server")
	workstation := newLabel(e.org, "role", "workstation")
	environment := newLabel(e.org, "env", "production")
	for _, label := range []Label{fileServer, workstation, environment} {
		require.NoError(t, s.CreateLabel(e.ctx, label))
	}

	require.NoError(t, s.AssignTag(e.ctx, e.device, fileServer.ID, "ivan"))
	require.NoError(t, s.AssignTag(e.ctx, e.device, environment.ID, "ivan"))
	require.NoError(t, s.AssignTag(e.ctx, e.device, workstation.ID, "ivan"))

	assert.Equal(t, map[string]string{"role": "workstation", "env": "production"},
		mustTagsFor(t, s, e.ctx, e.device))

	require.NoError(t, s.ClearTag(e.ctx, e.device, "role"))
	assert.Equal(t, map[string]string{"env": "production"}, mustTagsFor(t, s, e.ctx, e.device))
}

// Deleting a label a rule is aimed at would widen a threshold across an estate
// by removing a targeted override, and nothing would say it had happened. It is
// refused while the aim exists.
func TestDeletingALabelARuleAimsAtIsRefused(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	fileServer := newLabel(e.org, "role", "file-server")
	require.NoError(t, s.CreateLabel(e.ctx, fileServer))
	require.NoError(t, s.AssignTag(e.ctx, e.device, fileServer.ID, "ivan"))

	aimed := targeted(orgBinding(e.org, "disk-critical", threshold(95)),
		Selector{"role": "file-server"}, 10)
	require.NoError(t, s.UpsertBinding(e.ctx, cat, aimed))

	err := s.DeleteLabel(e.ctx, fileServer.ID)
	require.ErrorIs(t, err, ErrLabelInUse)
	assert.Len(t, mustListLabels(t, s, e.ctx, e.org), 1, "a refused delete must leave the label")
	assert.Equal(t, map[string]string{"role": "file-server"}, mustTagsFor(t, s, e.ctx, e.device))

	// With nothing aimed at it, the delete goes through and takes its
	// assignments with it — explicitly, rather than leaving machines carrying a
	// value the list no longer offers.
	require.NoError(t, s.DeleteBinding(e.ctx, aimed.ID))
	require.NoError(t, s.DeleteLabel(e.ctx, fileServer.ID))
	assert.Empty(t, mustListLabels(t, s, e.ctx, e.org))
	assert.Empty(t, mustTagsFor(t, s, e.ctx, e.device))
}

// A label aimed at by another customer's rule is not in use here. The selector
// is stored as a bare key and value, so a check that forgot the customer would
// refuse a delete because somebody else uses the same word.
func TestAnotherCustomersAimDoesNotHoldALabel(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	require.NoError(t, s.UpsertBinding(e.ctx, cat,
		targeted(orgBinding(other, "disk-critical", threshold(95)),
			Selector{"role": "file-server"}, 10)))

	fileServer := newLabel(e.org, "role", "file-server")
	require.NoError(t, s.CreateLabel(e.ctx, fileServer))
	require.NoError(t, s.DeleteLabel(e.ctx, fileServer.ID))
	assert.Empty(t, mustListLabels(t, s, e.ctx, e.org))
}

// Assignments are readable per customer, which is what the bulk-assignment
// screen lists and what a targeted binding is checked against.
func TestListingWhichMachinesCarryWhichLabel(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := newLabel(e.org, "role", "file-server")
	require.NoError(t, s.CreateLabel(e.ctx, fileServer))

	second := testutil.SeedDevice(t, e.ctx, e.store, e.site)
	require.NoError(t, s.AssignTag(e.ctx, e.device, fileServer.ID, "ivan"))
	require.NoError(t, s.AssignTag(e.ctx, second.ID, fileServer.ID, "ivan"))

	assignments, err := s.ListTagAssignments(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]map[string]string{
		e.device:  {"role": "file-server"},
		second.ID: {"role": "file-server"},
	}, assignments)
}

// A duplicate label is the same label. A list that holds `production` twice is a
// list where half an estate is targeted by one row and half by the other.
func TestALabelIsCreatedOnce(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	require.NoError(t, s.CreateLabel(e.ctx, newLabel(e.org, "env", "production")))
	require.ErrorIs(t, s.CreateLabel(e.ctx, newLabel(e.org, "env", "production")), ErrLabelExists)
	assert.Len(t, mustListLabels(t, s, e.ctx, e.org), 1)
}

// A label outside its bounds never reaches a row: it travels to every agent that
// carries it and is matched against a selector, so both halves are bounded.
func TestLabelValidation(t *testing.T) {
	t.Parallel()

	long := make([]byte, maxSelectorValueLen+1)
	for i := range long {
		long[i] = 'x'
	}
	org := uuid.New()

	for _, tc := range []struct {
		name  string
		label Label
	}{
		{"no key", newLabel(org, "", "production")},
		{"no value", newLabel(org, "env", "")},
		{"key too long", newLabel(org, string(long), "production")},
		{"value too long", newLabel(org, "env", string(long))},
		{"no customer", newLabel(uuid.Nil, "env", "production")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, ValidateLabel(tc.label), ErrInvalidLabel)
		})
	}

	require.NoError(t, ValidateLabel(newLabel(org, "env", "production")))
}

// Another tenant cannot read or write these rows even by naming the customer.
func TestTagsDenyCrossTenantAccess(t *testing.T) {
	t.Parallel()

	store := testutil.NewTestStore(t)
	ctxA := dbtx.WithDefaultTenant(context.Background(), false)
	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])

	siteA := testutil.SeedSite(t, ctxA, store)
	deviceA := testutil.SeedDevice(t, ctxA, store, siteA.ID)
	s := NewStore(store.DB())

	label := newLabel(siteA.OrganizationID, "role", "file-server")
	require.NoError(t, s.CreateLabel(ctxA, label))
	require.NoError(t, s.AssignTag(ctxA, deviceA.ID, label.ID, "ivan"))

	assert.Empty(t, mustListLabels(t, s, ctxB, siteA.OrganizationID))
	assert.Empty(t, mustTagsFor(t, s, ctxB, deviceA.ID))
	require.Error(t, s.CreateLabel(ctxB, newLabel(siteA.OrganizationID, "env", "production")))
}

// A read with no tenant on the context is refused rather than answered.
func TestTagsRequireTenantScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bare := context.Background()

	_, err := s.ListLabels(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = s.TagsFor(bare, e.device)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = s.ListTagAssignments(bare, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	assert.ErrorIs(t, s.CreateLabel(bare, newLabel(e.org, "env", "production")), dbtx.ErrTenantRequired)
}

// A tag override and a site override both matching one machine is settled by the
// rung, never by the tag: the narrower level always wins, and precedence only
// settles ties inside a level.
func TestASiteOverrideBeatsACustomerWideTagOverride(t *testing.T) {
	t.Parallel()

	def := diskCritical(t)
	org, site := uuid.New(), uuid.New()
	device := Device{
		Scope: settings.Scope{
			DeviceID:       uuid.New(),
			SiteID:         site,
			OrganizationID: org,
		},
		Tags: map[string]string{"role": "file-server"},
	}

	tagged := targeted(orgBinding(org, def.ID, threshold(80)), Selector{"role": "file-server"}, 50)
	atSite := newBinding(org, def.ID, settings.LevelSite, site, threshold(95))

	got := Resolve(def, device, []Binding{tagged, atSite})
	assert.InEpsilon(t, 95.0, got.Threshold,
		0.0001, "the site is narrower than the customer, whatever the tag's precedence")
}

// newLabel builds a label for one customer.
func newLabel(org uuid.UUID, key, value string) Label {
	return Label{ID: uuid.New(), OrganizationID: org, Key: key, Value: value}
}

// mustListLabels reads one customer's label list.
func mustListLabels(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Label {
	t.Helper()
	got, err := s.ListLabels(ctx, org)
	require.NoError(t, err)
	return got
}

// mustTagsFor reads one machine's labels.
func mustTagsFor(t *testing.T, s *Store, ctx context.Context, device uuid.UUID) map[string]string {
	t.Helper()
	got, err := s.TagsFor(ctx, device)
	require.NoError(t, err)
	return got
}

// mustCatalogue returns the embedded pack.
func mustCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	cat, err := Embedded()
	require.NoError(t, err)
	return cat
}
