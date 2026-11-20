package organization_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/organization"
)

// emptyTenant is a store and a tenant with no customer yet, so the next one made is its first.
type emptyTenant struct {
	repo  *organization.PostgresOrganizations
	store *db.PostgresStore
	ctx   context.Context
}

func newEmptyTenant(t *testing.T) emptyTenant {
	t.Helper()
	repo, store, _ := newFixture(t)
	_, ctx := seedBareTenant(t, store)
	return emptyTenant{repo: repo, store: store, ctx: ctx}
}

func (e emptyTenant) create(t *testing.T, name string) uuid.UUID {
	t.Helper()
	org := &organization.Organization{ID: uuid.New(), Name: name}
	require.NoError(t, e.repo.Create(e.ctx, org))
	return org.ID
}

// exec runs one statement inside the tenant's boundary.
func (e emptyTenant) exec(t *testing.T, statement string, args ...any) error {
	t.Helper()
	return dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, statement, args...)
		return err
	})
}

func (e emptyTenant) assertDefault(t *testing.T, want uuid.UUID) {
	t.Helper()
	got, err := e.repo.EnsureDefault(e.ctx)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestTheFirstCustomerStaysTheDefaultWhenALaterOneReadsOlder(t *testing.T) {
	t.Parallel()
	tenant := newEmptyTenant(t)

	contoso := tenant.create(t, "Contoso")
	fabrikam := tenant.create(t, "Fabrikam")
	// The server's clock stepped back between the two creates.
	require.NoError(t, tenant.exec(t, backdateQuery, fabrikam))

	tenant.assertDefault(t, contoso)
}

func TestDeletingTheDefaultHandsItToTheOldestCustomerLeft(t *testing.T) {
	t.Parallel()
	tenant := newEmptyTenant(t)

	contoso := tenant.create(t, "Contoso")
	fabrikam := tenant.create(t, "Fabrikam")
	tenant.create(t, "Northwind")
	require.NoError(t, tenant.repo.Delete(tenant.ctx, contoso))

	tenant.assertDefault(t, fabrikam)
	listed, err := tenant.repo.List(tenant.ctx, true)
	require.NoError(t, err)
	assert.Len(t, listed, 2, "handing on the default makes no customer of its own")
}

func TestDeletingAnotherCustomerLeavesTheDefault(t *testing.T) {
	t.Parallel()
	tenant := newEmptyTenant(t)

	contoso := tenant.create(t, "Contoso")
	fabrikam := tenant.create(t, "Fabrikam")
	require.NoError(t, tenant.exec(t, backdateQuery, tenant.create(t, "Northwind")))
	require.NoError(t, tenant.repo.Delete(tenant.ctx, fabrikam))

	tenant.assertDefault(t, contoso)
}

func TestCustomersMadeAtOnceInAnEmptyTenantAreAllKept(t *testing.T) {
	t.Parallel()
	tenant := newEmptyTenant(t)

	const creates = 8
	errs := make(chan error, creates)
	made := make(chan uuid.UUID, creates)
	for i := range creates {
		go func() {
			org := &organization.Organization{ID: uuid.New(), Name: fmt.Sprintf("Customer %d", i)}
			made <- org.ID
			errs <- tenant.repo.Create(tenant.ctx, org)
		}()
	}
	racing := make([]uuid.UUID, 0, creates)
	for range creates {
		racing = append(racing, <-made)
		assert.NoError(t, <-errs, "a create racing the tenant's first is kept, never read as a name clash")
	}

	got, err := tenant.repo.EnsureDefault(tenant.ctx)
	require.NoError(t, err)
	assert.Contains(t, racing, got, "one of the racing customers is the default")
	listed, err := tenant.repo.List(tenant.ctx, true)
	require.NoError(t, err)
	assert.Len(t, listed, creates)
}

func TestTheDatabaseHoldsOneDefaultPerTenant(t *testing.T) {
	t.Parallel()
	tenant := newEmptyTenant(t)

	tenant.create(t, "Contoso")
	fabrikam := tenant.create(t, "Fabrikam")

	err := tenant.exec(t, `UPDATE organizations SET is_default = TRUE
			  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`, fabrikam)
	assert.Error(t, err, "a second default in one tenant is refused")
}

// backdateQuery moves a customer's creation an hour back, as a clock stepping back reads it.
const backdateQuery = `UPDATE organizations SET created_at = created_at - INTERVAL '1 hour'
		  WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`
