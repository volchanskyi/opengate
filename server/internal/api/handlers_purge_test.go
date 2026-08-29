package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// fakePurger records purge calls and returns canned jobs.
type fakePurger struct {
	devicePurged []uuid.UUID
	tenantPurged []uuid.UUID
	ranJobs      []uuid.UUID
	bgJobs       []uuid.UUID
}

func (f *fakePurger) PurgeDevice(_ context.Context, tenantID, deviceID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	f.devicePurged = append(f.devicePurged, deviceID)
	return &lifecycle.PurgeJob{ID: uuid.New(), TenantID: tenantID, DeviceID: &deviceID, Scope: lifecycle.ScopeDevice, State: lifecycle.StateRequested}, nil
}

func (f *fakePurger) PurgeTenant(_ context.Context, tenantID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	f.tenantPurged = append(f.tenantPurged, tenantID)
	return &lifecycle.PurgeJob{ID: uuid.New(), TenantID: tenantID, Scope: lifecycle.ScopeTenant, State: lifecycle.StateRequested}, nil
}

func (f *fakePurger) Run(_ context.Context, job *lifecycle.PurgeJob) error {
	f.ranJobs = append(f.ranJobs, job.ID)
	return nil
}

func (f *fakePurger) RunInBackground(job *lifecycle.PurgeJob) {
	f.bgJobs = append(f.bgJobs, job.ID)
}

// fakeJobReader returns a fixed job map.
type fakeJobReader struct {
	jobs map[uuid.UUID]*lifecycle.PurgeJob
}

func (f *fakeJobReader) GetJob(_ context.Context, id uuid.UUID) (*lifecycle.PurgeJob, error) {
	job, ok := f.jobs[id]
	if !ok {
		return nil, lifecycle.ErrJobNotFound
	}
	return job, nil
}

func newPurgeTestServer(t *testing.T, purger DevicePurger, jobs PurgeJobReader) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Purger:         purger,
		PurgeJobs:      jobs,
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})
	return srv, cfg
}

func TestDeleteDeviceRunsPurgeWhenWired(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, token := seedTestUser(t, srv, cfg, "admin-purge@example.com", true)

	// Seed an ungrouped device so the admin owner check passes.
	ctx := testTenantContext(t)
	dev := &device.Device{ID: uuid.New(), Hostname: "doomed", OS: "linux", Status: device.StatusOffline}
	require.NoError(t, srv.devices.Upsert(ctx, dev))

	w := doRequest(srv, http.MethodDelete, "/api/v1/devices/"+dev.ID.String(), token, nil)
	assert.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, purger.devicePurged, 1, "delete must route through the purge orchestrator")
	assert.Equal(t, dev.ID, purger.devicePurged[0])
	assert.Len(t, purger.ranJobs, 1, "device purge runs in-request")
}

func TestPurgeTenantAdminOnly(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, userToken := seedTestUser(t, srv, cfg, "user-purge@example.com", false)

	w := doRequest(srv, http.MethodPost, "/api/v1/tenants/"+dbtx.DefaultTenantID.String()+"/purge", userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "non-admin cannot purge a tenant")
	assert.Empty(t, purger.tenantPurged)
}

func TestPurgeTenantCrossTenantDenied(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-cross@example.com", true)

	// The admin's tenant is the default tenant; purging a different tenant must be denied.
	other := uuid.New()
	w := doRequest(srv, http.MethodPost, "/api/v1/tenants/"+other.String()+"/purge", adminToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "admin cannot purge another tenant")
	assert.Empty(t, purger.tenantPurged)
}

func TestPurgeTenantAcceptsAndRunsAsync(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-ok@example.com", true)

	w := doRequest(srv, http.MethodPost, "/api/v1/tenants/"+dbtx.DefaultTenantID.String()+"/purge", adminToken, nil)
	assert.Equal(t, http.StatusAccepted, w.Code)
	require.Len(t, purger.tenantPurged, 1)
	assert.Equal(t, dbtx.DefaultTenantID, purger.tenantPurged[0])
	assert.Len(t, purger.bgJobs, 1, "tenant purge runs asynchronously")
}

func TestGetPurgeJobScopedToTenant(t *testing.T) {
	otherTenantJob := &lifecycle.PurgeJob{ID: uuid.New(), TenantID: uuid.New(), Scope: lifecycle.ScopeTenant, State: lifecycle.StateComplete}
	ownJob := &lifecycle.PurgeJob{ID: uuid.New(), TenantID: dbtx.DefaultTenantID, Scope: lifecycle.ScopeTenant, State: lifecycle.StateComplete}
	reader := &fakeJobReader{jobs: map[uuid.UUID]*lifecycle.PurgeJob{otherTenantJob.ID: otherTenantJob, ownJob.ID: ownJob}}
	srv, cfg := newPurgeTestServer(t, &fakePurger{}, reader)
	_, userToken := seedTestUser(t, srv, cfg, "user-job@example.com", false)

	// Own-tenant job is visible.
	w := doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+ownJob.ID.String(), userToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	// Another tenant's job is forbidden to a non-admin.
	w = doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+otherTenantJob.ID.String(), userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// A missing job is 404.
	w = doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+uuid.New().String(), userToken, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// A delete with no purge orchestrator wired is refused rather than served by a
// plain row delete. The fallback that used to stand here recorded no tombstone
// and no purge job and never erased the device's alerts, so the incident counts
// a foreign key cannot repair were left describing a machine that is gone.
// Refusing keeps the erasure guarantee and the delete together.
func TestDeleteDeviceRefusedWithoutPurger(t *testing.T) {
	srv, cfg := newPurgeTestServer(t, nil, nil)
	_, token := seedTestUser(t, srv, cfg, "admin-nopurger@example.com", true)

	ctx := testTenantContext(t)
	dev := &device.Device{ID: uuid.New(), Hostname: "survivor", OS: "linux", Status: device.StatusOffline}
	require.NoError(t, srv.devices.Upsert(ctx, dev))

	w := doRequest(srv, http.MethodDelete, "/api/v1/devices/"+dev.ID.String(), token, nil)
	assert.Equal(t, http.StatusForbidden, w.Code,
		"a delete that cannot erase the device's telemetry is refused")

	got, err := srv.devices.Get(ctx, dev.ID)
	require.NoError(t, err, "the device is still there to be deleted once a purger is wired")
	assert.Equal(t, dev.ID, got.ID)
}
