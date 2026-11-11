package device_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// presenceGrace is how old an unconnected online row grows before a sweep takes it offline.
const presenceGrace = time.Minute

// seedMachine registers a machine in the scope's tenant with the given status.
func seedMachine(t *testing.T, ctx context.Context, devices device.Repository, status device.DeviceStatus) device.DeviceID {
	t.Helper()
	d := &device.Device{ID: uuid.New(), Hostname: "contoso-" + uuid.NewString()[:8], OS: "linux", Status: status}
	require.NoError(t, devices.Upsert(ctx, d))
	return d.ID
}

// ageRow moves a row's last-seen and updated-at back by age, as time passing would.
func ageRow(t *testing.T, ctx context.Context, store *db.PostgresStore, id device.DeviceID, age time.Duration) {
	t.Helper()
	require.NoError(t, dbtx.Scoped(ctx, store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`UPDATE devices SET last_seen = NOW() - make_interval(secs => $2),
			                    updated_at = NOW() - make_interval(secs => $2)
			  WHERE id = $1`, id, age.Seconds())
		return err
	}))
}

func TestThePresenceSweepRefreshesConnectedMachinesAndMarksTheRestOffline(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	connected := seedMachine(t, ctx, devices, device.StatusOnline)
	ageRow(t, ctx, store, connected, 10*time.Minute)
	departed := seedMachine(t, ctx, devices, device.StatusOnline)
	ageRow(t, ctx, store, departed, 2*time.Minute)
	justArrived := seedMachine(t, ctx, devices, device.StatusOnline)
	ageRow(t, ctx, store, justArrived, 10*time.Second)
	longGone := seedMachine(t, ctx, devices, device.StatusOffline)
	ageRow(t, ctx, store, longGone, time.Hour)

	before := map[device.DeviceID]*device.Device{}
	for _, id := range []device.DeviceID{connected, departed, justArrived, longGone} {
		d, err := devices.Get(ctx, id)
		require.NoError(t, err)
		before[id] = d
	}

	refreshed, offline, err := devices.RefreshPresence(ctx, []device.DeviceID{connected}, presenceGrace)
	require.NoError(t, err)
	assert.Equal(t, 1, refreshed, "one connected machine has its last-seen refreshed")
	assert.Equal(t, 1, offline, "one online row with no connection is past the grace")

	after := func(id device.DeviceID) *device.Device {
		d, err := devices.Get(ctx, id)
		require.NoError(t, err)
		return d
	}
	assert.Equal(t, device.StatusOnline, after(connected).Status, "a connected machine keeps its status")
	assert.True(t, after(connected).LastSeen.After(before[connected].LastSeen.Add(9*time.Minute)),
		"a connected machine's last-seen is refreshed to now")

	assert.Equal(t, device.StatusOffline, after(departed).Status,
		"an online row with no connection, older than the grace, goes offline")
	assert.WithinDuration(t, before[departed].LastSeen, after(departed).LastSeen, time.Millisecond,
		"going offline keeps the time the machine was last seen")

	assert.Equal(t, device.StatusOnline, after(justArrived).Status,
		"a machine that connected after the list was taken stays online")
	assert.WithinDuration(t, before[justArrived].UpdatedAt, after(justArrived).UpdatedAt, time.Millisecond)

	assert.WithinDuration(t, before[longGone].UpdatedAt, after(longGone).UpdatedAt, time.Millisecond,
		"an offline machine with no connection is left alone")
}

func TestThePresenceSweepTouchesOnlyItsOwnTenantsRows(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	contoso := dbtx.WithDefaultTenant(context.Background(), false)
	fabrikamID := uuid.New()
	testutil.EnsureTenant(t, context.Background(), store, fabrikamID, "Fabrikam")
	fabrikam := dbtx.WithTenant(context.Background(), fabrikamID, false)

	contosoStale := seedMachine(t, contoso, devices, device.StatusOnline)
	ageRow(t, contoso, store, contosoStale, 2*time.Minute)
	fabrikamStale := seedMachine(t, fabrikam, devices, device.StatusOnline)
	ageRow(t, fabrikam, store, fabrikamStale, 2*time.Minute)
	fabrikamConnected := seedMachine(t, fabrikam, devices, device.StatusOnline)
	ageRow(t, fabrikam, store, fabrikamConnected, 10*time.Minute)

	refreshed, offline, err := devices.RefreshPresence(contoso, []device.DeviceID{fabrikamConnected}, presenceGrace)
	require.NoError(t, err)
	assert.Equal(t, 0, refreshed, "another tenant's machine is never refreshed")
	assert.Equal(t, 1, offline, "only the sweeping tenant's stale row goes offline")

	stale, err := devices.Get(fabrikam, fabrikamStale)
	require.NoError(t, err)
	assert.Equal(t, device.StatusOnline, stale.Status, "another tenant's stale row waits for its own sweep")
	connected, err := devices.Get(fabrikam, fabrikamConnected)
	require.NoError(t, err)
	assert.True(t, connected.LastSeen.Before(time.Now().Add(-9*time.Minute)),
		"another tenant's connected machine keeps the last-seen its own sweep wrote")
	gone, err := devices.Get(contoso, contosoStale)
	require.NoError(t, err)
	assert.Equal(t, device.StatusOffline, gone.Status)
}

func TestThePresenceSweepWithNoConnectedMachinesStillReconcilesStaleRows(t *testing.T) {
	t.Parallel()
	devices, _, _, store := newRepos(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	stale := seedMachine(t, ctx, devices, device.StatusOnline)
	ageRow(t, ctx, store, stale, 2*time.Minute)

	refreshed, offline, err := devices.RefreshPresence(ctx, nil, presenceGrace)
	require.NoError(t, err)
	assert.Equal(t, 0, refreshed)
	assert.Equal(t, 1, offline, "a tenant whose last machine left still has its stale row reconciled")
}

func TestThePresenceSweepRequiresATenant(t *testing.T) {
	t.Parallel()
	devices, _, _, _ := newRepos(t)
	_, _, err := devices.RefreshPresence(context.Background(), nil, presenceGrace)
	require.ErrorIs(t, err, dbtx.ErrTenantRequired)
}
