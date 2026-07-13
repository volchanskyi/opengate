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
	orgPurged    []uuid.UUID
	ranJobs      []uuid.UUID
	bgJobs       []uuid.UUID
}

func (f *fakePurger) PurgeDevice(_ context.Context, orgID, deviceID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	f.devicePurged = append(f.devicePurged, deviceID)
	return &lifecycle.PurgeJob{ID: uuid.New(), OrgID: orgID, DeviceID: &deviceID, Scope: lifecycle.ScopeDevice, State: lifecycle.StateRequested}, nil
}

func (f *fakePurger) PurgeOrg(_ context.Context, orgID uuid.UUID, _ *uuid.UUID) (*lifecycle.PurgeJob, error) {
	f.orgPurged = append(f.orgPurged, orgID)
	return &lifecycle.PurgeJob{ID: uuid.New(), OrgID: orgID, Scope: lifecycle.ScopeOrg, State: lifecycle.StateRequested}, nil
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
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
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

func TestPurgeOrgAdminOnly(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, userToken := seedTestUser(t, srv, cfg, "user-purge@example.com", false)

	w := doRequest(srv, http.MethodPost, "/api/v1/orgs/"+dbtx.DefaultOrgID.String()+"/purge", userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "non-admin cannot purge a tenant")
	assert.Empty(t, purger.orgPurged)
}

func TestPurgeOrgCrossTenantDenied(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-cross@example.com", true)

	// The admin's org is the default org; purging a different org must be denied.
	other := uuid.New()
	w := doRequest(srv, http.MethodPost, "/api/v1/orgs/"+other.String()+"/purge", adminToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code, "admin cannot purge another tenant")
	assert.Empty(t, purger.orgPurged)
}

func TestPurgeOrgAcceptsAndRunsAsync(t *testing.T) {
	purger := &fakePurger{}
	srv, cfg := newPurgeTestServer(t, purger, nil)
	_, adminToken := seedTestUser(t, srv, cfg, "admin-ok@example.com", true)

	w := doRequest(srv, http.MethodPost, "/api/v1/orgs/"+dbtx.DefaultOrgID.String()+"/purge", adminToken, nil)
	assert.Equal(t, http.StatusAccepted, w.Code)
	require.Len(t, purger.orgPurged, 1)
	assert.Equal(t, dbtx.DefaultOrgID, purger.orgPurged[0])
	assert.Len(t, purger.bgJobs, 1, "tenant purge runs asynchronously")
}

func TestGetPurgeJobScopedToTenant(t *testing.T) {
	otherOrgJob := &lifecycle.PurgeJob{ID: uuid.New(), OrgID: uuid.New(), Scope: lifecycle.ScopeOrg, State: lifecycle.StateComplete}
	ownJob := &lifecycle.PurgeJob{ID: uuid.New(), OrgID: dbtx.DefaultOrgID, Scope: lifecycle.ScopeOrg, State: lifecycle.StateComplete}
	reader := &fakeJobReader{jobs: map[uuid.UUID]*lifecycle.PurgeJob{otherOrgJob.ID: otherOrgJob, ownJob.ID: ownJob}}
	srv, cfg := newPurgeTestServer(t, &fakePurger{}, reader)
	_, userToken := seedTestUser(t, srv, cfg, "user-job@example.com", false)

	// Own-org job is visible.
	w := doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+ownJob.ID.String(), userToken, nil)
	assert.Equal(t, http.StatusOK, w.Code)

	// Another tenant's job is forbidden to a non-admin.
	w = doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+otherOrgJob.ID.String(), userToken, nil)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// A missing job is 404.
	w = doRequest(srv, http.MethodGet, "/api/v1/purge-jobs/"+uuid.New().String(), userToken, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}
