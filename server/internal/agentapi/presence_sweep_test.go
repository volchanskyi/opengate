package agentapi

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	ogtestutil "github.com/volchanskyi/opengate/server/internal/testutil"
)

var errDatabaseDown = errors.New("database unreachable")

// unreachableDevices answers every call with errDatabaseDown and counts the calls.
type unreachableDevices struct{ calls atomic.Int64 }

func (u *unreachableDevices) fail() error { u.calls.Add(1); return errDatabaseDown }

func (u *unreachableDevices) Upsert(context.Context, *device.Device) error { return u.fail() }
func (u *unreachableDevices) Get(context.Context, device.DeviceID) (*device.Device, error) {
	return nil, u.fail()
}
func (u *unreachableDevices) GetByAMTUUID(context.Context, uuid.UUID) (*device.Device, error) {
	return nil, u.fail()
}
func (u *unreachableDevices) TenantForDevice(context.Context, device.DeviceID) (uuid.UUID, error) {
	return uuid.Nil, u.fail()
}
func (u *unreachableDevices) List(context.Context, device.Filter) ([]*device.Device, error) {
	return nil, u.fail()
}
func (u *unreachableDevices) Delete(context.Context, device.DeviceID) error { return u.fail() }
func (u *unreachableDevices) UpdateSite(context.Context, device.DeviceID, device.SiteID) error {
	return u.fail()
}
func (u *unreachableDevices) UpdateOrganization(context.Context, device.DeviceID, device.OrganizationID) error {
	return u.fail()
}
func (u *unreachableDevices) SetStatus(context.Context, device.DeviceID, device.DeviceStatus) error {
	return u.fail()
}
func (u *unreachableDevices) ResetAllStatuses(context.Context) error { return u.fail() }
func (u *unreachableDevices) RefreshPresence(context.Context, []device.DeviceID, time.Duration) (int, int, error) {
	return 0, 0, u.fail()
}
func (u *unreachableDevices) SetMaintenance(context.Context, device.DeviceID, bool, uuid.UUID, string) error {
	return u.fail()
}
func (u *unreachableDevices) RecordSharedIdentity(context.Context, device.DeviceID, []string, time.Time) error {
	return u.fail()
}
func (u *unreachableDevices) Counts(context.Context, device.OrganizationID) (device.Counts, error) {
	return device.Counts{}, u.fail()
}

// offlineWriteFails is a Postgres device store whose offline writes fail, as in a database blip.
type offlineWriteFails struct{ device.Repository }

func (o offlineWriteFails) SetStatus(ctx context.Context, id device.DeviceID, status device.DeviceStatus) error {
	if status == device.StatusOffline {
		return errDatabaseDown
	}
	return o.Repository.SetStatus(ctx, id, status)
}

// sweepFailsFor is a Postgres device store whose sweep statement fails for one tenant.
type sweepFailsFor struct {
	device.Repository
	tenant uuid.UUID
}

func (s sweepFailsFor) RefreshPresence(ctx context.Context, ids []device.DeviceID, grace time.Duration) (int, int, error) {
	if tenant, _ := dbtx.TenantFromContext(ctx); tenant.TenantID == s.tenant {
		return 0, 0, errDatabaseDown
	}
	return s.Repository.RefreshPresence(ctx, ids, grace)
}

func newPresenceServer(t *testing.T, devices device.Repository) (*AgentServer, *appmetrics.Metrics, *bytes.Buffer) {
	t.Helper()
	srv := newTestAgentServer(t)
	srv.devices = devices
	metrics := appmetrics.NewMetrics(appmetrics.NewRegistry())
	srv.metrics = metrics
	var logs bytes.Buffer
	srv.logger = slog.New(slog.NewTextHandler(&logs, nil))
	return srv, metrics, &logs
}

// onlineMachine seeds an online row in ctx's tenant and returns its id.
func onlineMachine(t *testing.T, ctx context.Context, devices device.Repository) device.DeviceID {
	t.Helper()
	d := &device.Device{ID: uuid.New(), Hostname: "contoso-" + uuid.NewString()[:8], OS: "linux",
		Status: device.StatusOnline}
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

func statusOf(t *testing.T, ctx context.Context, devices device.Repository, id device.DeviceID) *device.Device {
	t.Helper()
	d, err := devices.Get(ctx, id)
	require.NoError(t, err)
	return d
}

func TestAHeartbeatWritesNothingToTheDatabase(t *testing.T) {
	devices := &unreachableDevices{}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.devices = devices
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:      protocol.MsgAgentHeartbeat,
		Timestamp: time.Now().Unix(),
	})

	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)),
		"a heartbeat survives a database that answers nothing")
	assert.Zero(t, devices.calls.Load(), "a heartbeat makes no database call")
}

func TestAFailedStatusWriteNeverEndsTheConnection(t *testing.T) {
	srv, metrics, logs := newPresenceServer(t, &unreachableDevices{})
	departing := &AgentConn{DeviceID: protocol.DeviceID(uuid.New())}
	srv.registerConn(context.Background(), departing, "contoso-workstation")

	srv.releaseDeviceStatus(departing, "contoso-workstation", srv.logger)

	assert.Equal(t, 0, srv.ConnectedAgentCount(), "teardown completes past a failed offline write")
	assert.Nil(t, srv.GetAgent(departing.DeviceID))
	assert.InDelta(t, 1, testutil.ToFloat64(metrics.PresenceWriteFailuresTotal.WithLabelValues("disconnect")), 0,
		"the failed offline write is counted")
	assert.Contains(t, logs.String(), "set device offline", "the failed offline write is logged")
}

func TestThePresenceSweepReconcilesAMissedOfflineWrite(t *testing.T) {
	store := ogtestutil.NewTestStore(t)
	rows := ogtestutil.NewTestDevices(t, store)
	srv, metrics, _ := newPresenceServer(t, offlineWriteFails{rows})

	fabrikamID := uuid.New()
	ogtestutil.EnsureTenant(t, context.Background(), store, fabrikamID, "Fabrikam")
	fabrikam := dbtx.WithTenant(context.Background(), fabrikamID, false)
	contoso := dbtx.WithDefaultTenant(context.Background(), false)

	fabrikamLast := onlineMachine(t, fabrikam, rows)
	contosoLeaving := onlineMachine(t, contoso, rows)
	contosoStaying := onlineMachine(t, contoso, rows)

	leavingFabrikam := &AgentConn{DeviceID: fabrikamLast, TenantID: fabrikamID}
	leavingContoso := &AgentConn{DeviceID: contosoLeaving}
	srv.registerConn(fabrikam, leavingFabrikam, "fabrikam-desk-01")
	srv.registerConn(contoso, leavingContoso, "contoso-desk-01")
	srv.registerConn(contoso, &AgentConn{DeviceID: contosoStaying}, "contoso-desk-02")
	srv.releaseDeviceStatus(leavingFabrikam, "fabrikam-desk-01", srv.logger)
	srv.releaseDeviceStatus(leavingContoso, "contoso-desk-01", srv.logger)

	require.Equal(t, device.StatusOnline, statusOf(t, fabrikam, rows, fabrikamLast).Status,
		"the offline write failed, so the row still reads online")
	ageRow(t, fabrikam, store, fabrikamLast, 2*time.Minute)
	ageRow(t, contoso, store, contosoLeaving, 2*time.Minute)
	ageRow(t, contoso, store, contosoStaying, 10*time.Minute)

	offline, err := srv.SweepPresence(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, offline)
	assert.Equal(t, device.StatusOffline, statusOf(t, fabrikam, rows, fabrikamLast).Status,
		"a customer's last machine is reconciled although nothing of that customer is connected")
	assert.Equal(t, device.StatusOffline, statusOf(t, contoso, rows, contosoLeaving).Status)
	staying := statusOf(t, contoso, rows, contosoStaying)
	assert.Equal(t, device.StatusOnline, staying.Status, "a connected machine stays online")
	assert.WithinDuration(t, time.Now(), staying.LastSeen, 30*time.Second,
		"a connected machine's last-seen is refreshed")
	assert.InDelta(t, 2, testutil.ToFloat64(metrics.PresenceMarkedOfflineTotal), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(metrics.PresenceSweepsTotal.WithLabelValues("ok")), 0)
}

func TestAPresenceSweepThatFailsForOneTenantStillSweepsTheOthers(t *testing.T) {
	store := ogtestutil.NewTestStore(t)
	rows := ogtestutil.NewTestDevices(t, store)
	fabrikamID := uuid.New()
	ogtestutil.EnsureTenant(t, context.Background(), store, fabrikamID, "Fabrikam")
	srv, metrics, logs := newPresenceServer(t, sweepFailsFor{Repository: rows, tenant: fabrikamID})
	fabrikam := dbtx.WithTenant(context.Background(), fabrikamID, false)
	contoso := dbtx.WithDefaultTenant(context.Background(), false)

	srv.registerConn(fabrikam, &AgentConn{DeviceID: onlineMachine(t, fabrikam, rows), TenantID: fabrikamID}, "f")
	stale := onlineMachine(t, contoso, rows)
	ageRow(t, contoso, store, stale, 2*time.Minute)
	srv.registerConn(contoso, &AgentConn{DeviceID: onlineMachine(t, contoso, rows)}, "c")

	offline, err := srv.SweepPresence(context.Background())
	require.ErrorIs(t, err, errDatabaseDown)

	assert.Equal(t, 1, offline, "the healthy tenant is swept")
	assert.Equal(t, device.StatusOffline, statusOf(t, contoso, rows, stale).Status)
	assert.InDelta(t, 1, testutil.ToFloat64(metrics.PresenceWriteFailuresTotal.WithLabelValues("sweep")), 0)
	assert.InDelta(t, 1, testutil.ToFloat64(metrics.PresenceSweepsTotal.WithLabelValues("failed")), 0)
	assert.Contains(t, logs.String(), fabrikamID.String(), "the failing tenant is named in the log")
}
