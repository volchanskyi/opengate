package organization_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/organization"
)

// TestArchiveHidesFromTheWorkingSetWithoutDeleting covers retiring a customer:
// the row and everything under it stay, but the default listing no longer offers
// it.
func TestArchiveHidesFromTheWorkingSetWithoutDeleting(t *testing.T) {
	t.Parallel()
	repo, _, ctx := newFixture(t)

	retired := &organization.Organization{ID: uuid.New(), Name: "Retired Customer"}
	require.NoError(t, repo.Create(ctx, retired))
	require.NoError(t, repo.SetArchived(ctx, retired.ID, true))

	active, err := repo.List(ctx, false)
	require.NoError(t, err)
	assert.NotContains(t, ids(active), retired.ID)

	all, err := repo.List(ctx, true)
	require.NoError(t, err)
	assert.Contains(t, ids(all), retired.ID)

	got, err := repo.Get(ctx, retired.ID)
	require.NoError(t, err, "an archived customer is still readable by id")
	require.NotNil(t, got.ArchivedAt)

	require.NoError(t, repo.SetArchived(ctx, retired.ID, false))
	restored, err := repo.List(ctx, false)
	require.NoError(t, err)
	assert.Contains(t, ids(restored), retired.ID)
}

// TestRenameAndMissingRowErrors covers the ordinary edits plus the not-found
// half of each mutation.
func TestRenameAndMissingRowErrors(t *testing.T) {
	t.Parallel()
	repo, _, ctx := newFixture(t)

	org := &organization.Organization{ID: uuid.New(), Name: "Before"}
	require.NoError(t, repo.Create(ctx, org))
	require.NoError(t, repo.Rename(ctx, org.ID, "After"))

	got, err := repo.Get(ctx, org.ID)
	require.NoError(t, err)
	assert.Equal(t, "After", got.Name)

	missing := uuid.New()
	assert.ErrorIs(t, repo.Rename(ctx, missing, "Nothing"), organization.ErrNotFound)
	assert.ErrorIs(t, repo.SetArchived(ctx, missing, true), organization.ErrNotFound)
	assert.ErrorIs(t, repo.Delete(ctx, missing), organization.ErrNotFound)
	_, err = repo.Get(ctx, missing)
	assert.ErrorIs(t, err, organization.ErrNotFound)
}

// TestRepositoryRequiresTenantScope proves every operation fails closed when the
// caller carries no tenant, rather than reading or writing across the wall.
func TestRepositoryRequiresTenantScope(t *testing.T) {
	t.Parallel()
	repo, _, _ := newFixture(t)
	bare := context.Background()

	assert.ErrorIs(t, repo.Create(bare, &organization.Organization{ID: uuid.New(), Name: "Nowhere"}), dbtx.ErrTenantRequired)
	assert.ErrorIs(t, repo.Rename(bare, uuid.New(), "Nowhere"), dbtx.ErrTenantRequired)
	assert.ErrorIs(t, repo.SetArchived(bare, uuid.New(), true), dbtx.ErrTenantRequired)
	assert.ErrorIs(t, repo.Delete(bare, uuid.New()), dbtx.ErrTenantRequired)
	_, err := repo.Get(bare, uuid.New())
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = repo.List(bare, false)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = repo.EnsureDefault(bare)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
}
