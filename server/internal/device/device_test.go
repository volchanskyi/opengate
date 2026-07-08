package device_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"testing"
)

// seedOwner inserts a user we can use as Group.OwnerID without exercising
// auth-aggregate-specific helpers.
func seedOwner(t *testing.T, ctx context.Context, store *db.PostgresStore) uuid.UUID {
	t.Helper()
	u := testutil.SeedUser(t, ctx, store)
	return u.ID
}

func newRepos(t *testing.T) (device.Repository, device.GroupRepository, device.HardwareRepository, *db.PostgresStore) {
	t.Helper()
	store := testutil.NewTestStore(t)
	return testutil.NewTestDevices(t, store),
		testutil.NewTestGroups(t, store),
		testutil.NewTestHardware(t, store),
		store
}
