package device_test

import (
	"context"
	"database/sql"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
)

// clockSteppedBack is a tenant whose first customer, Contoso, reads younger than its second,
// Fabrikam, because the server's clock stepped back between the two creates.
type clockSteppedBack struct {
	ctx     context.Context
	contoso uuid.UUID
}

func newClockSteppedBack(t *testing.T, store *db.PostgresStore) clockSteppedBack {
	t.Helper()
	tenantID := uuid.New()
	_, err := store.DB().ExecContext(context.Background(),
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "Stepped "+tenantID.String()[:8])
	require.NoError(t, err)
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)

	contoso := newCustomer(t, ctx, store, "Contoso")
	fabrikam := newCustomer(t, ctx, store, "Fabrikam")
	require.NoError(t, dbtx.Scoped(ctx, store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE organizations SET created_at = created_at - INTERVAL '1 hour'
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, fabrikam)
		return err
	}))
	return clockSteppedBack{ctx: ctx, contoso: contoso}
}

func TestAMachineNamingNoCustomerLandsInTheDefaultWhateverTheClockRead(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	tenant := newClockSteppedBack(t, store)

	d := &device.Device{ID: uuid.New(), Hostname: "new-machine", Status: device.StatusOnline}
	testutil.SignIn(t, tenant.ctx, devices, d)

	got, err := devices.Get(tenant.ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.contoso, got.OrganizationID,
		"a machine naming no customer goes to the tenant's first customer, not the one reading oldest")
}

func TestARegisteringSiteCountsAgainstTheDefaultCustomer(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	tenant := newClockSteppedBack(t, store)
	dallas := newSite(t, tenant.ctx, store, tenant.contoso, "Dallas")

	d := &device.Device{ID: uuid.New(), SiteID: dallas.ID, Hostname: "dallas-fs01", Status: device.StatusOnline}
	testutil.SignIn(t, tenant.ctx, devices, d)

	got, err := devices.Get(tenant.ctx, d.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.contoso, got.OrganizationID)
	assert.Equal(t, dallas.ID, got.SiteID, "the default customer's own site is kept")
}

func TestASiteNamingNoCustomerGoesToTheDefault(t *testing.T) {
	t.Parallel()
	_, sites, _, store := newRepos(t)
	tenant := newClockSteppedBack(t, store)

	site := &device.Site{ID: uuid.New(), Name: "Head Office"}
	require.NoError(t, sites.Create(tenant.ctx, site))

	got, err := sites.Get(tenant.ctx, site.ID)
	require.NoError(t, err)
	assert.Equal(t, tenant.contoso, got.OrganizationID)
}
