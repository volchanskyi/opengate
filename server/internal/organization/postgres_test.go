package organization_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/organization"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func newFixture(t *testing.T) (*organization.PostgresOrganizations, *db.PostgresStore, context.Context) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return organization.NewPostgresOrganizations(store.DB()), store, dbtx.WithDefaultTenant(context.Background(), false)
}

// seedBareTenant inserts a tenant with no organization, a state the migration never leaves.
func seedBareTenant(t *testing.T, store *db.PostgresStore) (uuid.UUID, context.Context) {
	t.Helper()
	tenantID := uuid.New()
	_, err := store.DB().ExecContext(context.Background(),
		`INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "Bare "+tenantID.String()[:8])
	require.NoError(t, err)
	return tenantID, dbtx.WithTenant(context.Background(), tenantID, false)
}

func TestOrganizationBelongsToExactlyOneTenant(t *testing.T) {
	t.Parallel()
	repo, store, ctxA := newFixture(t)

	tenantB := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, tenantB, "Tenant "+tenantB.String()[:8])
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)

	inA := &organization.Organization{ID: uuid.New(), Name: "Contoso"}
	require.NoError(t, repo.Create(ctxA, inA))

	t.Run("visible in its own tenant", func(t *testing.T) {
		got, err := repo.Get(ctxA, inA.ID)
		require.NoError(t, err)
		assert.Equal(t, "Contoso", got.Name)
	})

	t.Run("not readable from another tenant", func(t *testing.T) {
		_, err := repo.Get(ctxB, inA.ID)
		assert.ErrorIs(t, err, organization.ErrNotFound)
	})

	t.Run("not listed in another tenant", func(t *testing.T) {
		listed, err := repo.List(ctxB, false)
		require.NoError(t, err)
		for _, o := range listed {
			assert.NotEqual(t, inA.ID, o.ID, "tenant B must not see tenant A's customer")
		}
	})

	t.Run("not renamable from another tenant", func(t *testing.T) {
		assert.ErrorIs(t, repo.Rename(ctxB, inA.ID, "Stolen"), organization.ErrNotFound)
		got, err := repo.Get(ctxA, inA.ID)
		require.NoError(t, err)
		assert.Equal(t, "Contoso", got.Name, "the name must survive the refused rename")
	})

	t.Run("not deletable from another tenant", func(t *testing.T) {
		assert.ErrorIs(t, repo.Delete(ctxB, inA.ID), organization.ErrNotFound)
		_, err := repo.Get(ctxA, inA.ID)
		require.NoError(t, err, "the row must survive the refused delete")
	})

	t.Run("the same name may exist in both tenants", func(t *testing.T) {
		inB := &organization.Organization{ID: uuid.New(), Name: "Contoso"}
		require.NoError(t, repo.Create(ctxB, inB), "customer names are unique per tenant, not globally")
	})
}

func TestCreateRejectsDuplicateNameWithinTenant(t *testing.T) {
	t.Parallel()
	repo, _, ctx := newFixture(t)

	require.NoError(t, repo.Create(ctx, &organization.Organization{ID: uuid.New(), Name: "Fabrikam"}))
	err := repo.Create(ctx, &organization.Organization{ID: uuid.New(), Name: "Fabrikam"})
	assert.ErrorIs(t, err, organization.ErrNameTaken)
}

func TestEnsureDefaultGivesATenantSomewhereToPutDevices(t *testing.T) {
	t.Parallel()
	repo, store, _ := newFixture(t)
	_, ctx := seedBareTenant(t, store)

	first, err := repo.EnsureDefault(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, first)

	second, err := repo.EnsureDefault(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, second, "asking twice must not create a second default")

	listed, err := repo.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, organization.DefaultName, listed[0].Name)
}

func TestEnsureDefaultKeepsAnExistingOrganization(t *testing.T) {
	t.Parallel()
	repo, store, _ := newFixture(t)
	_, ctx := seedBareTenant(t, store)

	existing := &organization.Organization{ID: uuid.New(), Name: "Northwind"}
	require.NoError(t, repo.Create(ctx, existing))

	got, err := repo.EnsureDefault(ctx)
	require.NoError(t, err)
	assert.Equal(t, existing.ID, got, "a tenant that already has a customer needs no default")
}

func ids(orgs []*organization.Organization) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, o.ID)
	}
	return out
}
