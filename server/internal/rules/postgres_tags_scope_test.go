package rules

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestLabelValidation(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", maxSelectorValueLen+1)
	org := uuid.New()

	for _, tc := range []struct {
		name  string
		label Label
	}{
		{"no key", newLabel(org, "", "production")},
		{"no value", newLabel(org, "env", "")},
		{"key too long", newLabel(org, long, "production")},
		{"value too long", newLabel(org, "env", long)},
		{"no customer", newLabel(uuid.Nil, "env", "production")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, ValidateLabel(tc.label), ErrInvalidLabel)
		})
	}

	require.NoError(t, ValidateLabel(newLabel(org, "env", "production")))
}

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

	e := estate{store: store, ctx: ctxA, org: siteA.OrganizationID, site: siteA.ID, device: deviceA.ID}
	label := createLabel(t, s, e, "role", "file-server")
	assignTags(t, s, e, deviceA.ID, label)

	assert.Empty(t, mustListLabels(t, s, ctxB, siteA.OrganizationID))
	assert.Empty(t, mustTagsFor(t, s, ctxB, deviceA.ID))
	require.Error(t, s.CreateLabel(ctxB, newLabel(siteA.OrganizationID, "env", "production")))
}

func TestTagsRequireTenantScope(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	bare := context.Background()

	for name, call := range map[string]func() error{
		"labels": func() error { _, err := s.ListLabels(bare, e.org); return err },
		"tags":   func() error { _, err := s.TagsFor(bare, e.device); return err },
		"assign": func() error { _, err := s.ListTagAssignments(bare, e.org); return err },
	} {
		assert.ErrorIs(t, call(), dbtx.ErrTenantRequired, name)
	}
	assert.ErrorIs(t, s.CreateLabel(bare, newLabel(e.org, "env", "production")), dbtx.ErrTenantRequired)
}

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
