package rules

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestLabelsBelongToOneCustomer(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := createLabel(t, s, e, "role", "file-server")

	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	otherSite := testutil.SeedSiteIn(t, e.ctx, e.store, other)
	otherDevice := testutil.SeedDeviceIn(t, e.ctx, e.store, other, otherSite.ID)

	assert.Equal(t, []Label{fileServer}, mustListLabels(t, s, e.ctx, e.org))
	assert.Empty(t, mustListLabels(t, s, e.ctx, other),
		"one customer's label list must not appear in another's")

	assignTags(t, s, e, e.device, fileServer)
	assert.Equal(t, map[string]string{"role": "file-server"}, mustTagsFor(t, s, e.ctx, e.device))

	err := s.AssignTag(e.ctx, otherDevice.ID, fileServer.ID, "ivan")
	require.ErrorIs(t, err, ErrLabelForeign)
	assert.Empty(t, mustTagsFor(t, s, e.ctx, otherDevice.ID))
}

func TestAssigningASecondValueForOneKeyReplacesTheFirst(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := createLabel(t, s, e, "role", "file-server")
	workstation := createLabel(t, s, e, "role", "workstation")
	environment := createLabel(t, s, e, "env", "production")

	assignTags(t, s, e, e.device, fileServer, environment, workstation)

	assert.Equal(t, map[string]string{"role": "workstation", "env": "production"},
		mustTagsFor(t, s, e.ctx, e.device))

	require.NoError(t, s.ClearTag(e.ctx, e.device, "role"))
	assert.Equal(t, map[string]string{"env": "production"}, mustTagsFor(t, s, e.ctx, e.device))
}

func TestDeletingALabelARuleAimsAtIsRefused(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	fileServer := createLabel(t, s, e, "role", "file-server")
	assignTags(t, s, e, e.device, fileServer)

	aimed := aimAtFileServers(e.org)
	require.NoError(t, s.UpsertBinding(e.ctx, cat, aimed))

	err := s.DeleteLabel(e.ctx, fileServer.ID)
	require.ErrorIs(t, err, ErrLabelInUse)
	assert.Len(t, mustListLabels(t, s, e.ctx, e.org), 1, "a refused delete must leave the label")
	assert.Equal(t, map[string]string{"role": "file-server"}, mustTagsFor(t, s, e.ctx, e.device))

	require.NoError(t, s.DeleteBinding(e.ctx, aimed.ID))
	require.NoError(t, s.DeleteLabel(e.ctx, fileServer.ID))
	assert.Empty(t, mustListLabels(t, s, e.ctx, e.org))
	assert.Empty(t, mustTagsFor(t, s, e.ctx, e.device))
}

func TestAnotherCustomersAimDoesNotHoldALabel(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)
	cat := mustCatalogue(t)

	other := testutil.SeedOrganization(t, e.ctx, e.store, "fabrikam")
	require.NoError(t, s.UpsertBinding(e.ctx, cat, aimAtFileServers(other)))

	fileServer := createLabel(t, s, e, "role", "file-server")
	require.NoError(t, s.DeleteLabel(e.ctx, fileServer.ID))
	assert.Empty(t, mustListLabels(t, s, e.ctx, e.org))
}

func TestListingWhichMachinesCarryWhichLabel(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	fileServer := createLabel(t, s, e, "role", "file-server")

	second := testutil.SeedDevice(t, e.ctx, e.store, e.site)
	assignTags(t, s, e, e.device, fileServer)
	assignTags(t, s, e, second.ID, fileServer)

	assignments, err := s.ListTagAssignments(e.ctx, e.org)
	require.NoError(t, err)
	assert.Equal(t, map[uuid.UUID]map[string]string{
		e.device:  {"role": "file-server"},
		second.ID: {"role": "file-server"},
	}, assignments)
}

func TestALabelIsCreatedOnce(t *testing.T) {
	t.Parallel()

	s, e := newEstate(t)

	createLabel(t, s, e, "env", "production")
	require.ErrorIs(t, s.CreateLabel(e.ctx, newLabel(e.org, "env", "production")), ErrLabelExists)
	assert.Len(t, mustListLabels(t, s, e.ctx, e.org), 1)
}

func newLabel(org uuid.UUID, key, value string) Label {
	return Label{ID: uuid.New(), OrganizationID: org, Key: key, Value: value}
}

func createLabel(t *testing.T, s *Store, e estate, key, value string) Label {
	t.Helper()
	label := newLabel(e.org, key, value)
	require.NoError(t, s.CreateLabel(e.ctx, label))
	return label
}

func assignTags(t *testing.T, s *Store, e estate, device uuid.UUID, labels ...Label) {
	t.Helper()
	for _, label := range labels {
		require.NoError(t, s.AssignTag(e.ctx, device, label.ID, "ivan"))
	}
}

func aimAtFileServers(org uuid.UUID) Binding {
	return targeted(orgBinding(org, "disk-critical", threshold(95)), Selector{"role": "file-server"}, 10)
}

func mustListLabels(t *testing.T, s *Store, ctx context.Context, org uuid.UUID) []Label {
	t.Helper()
	got, err := s.ListLabels(ctx, org)
	require.NoError(t, err)
	return got
}

func mustTagsFor(t *testing.T, s *Store, ctx context.Context, device uuid.UUID) map[string]string {
	t.Helper()
	got, err := s.TagsFor(ctx, device)
	require.NoError(t, err)
	return got
}

func mustCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	cat, err := Embedded()
	require.NoError(t, err)
	return cat
}
