package lifecycle

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func seedInvestigation(
	t *testing.T, f *orchestratorFixture, tenantID, organizationID uuid.UUID, devices []uuid.UUID,
) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	store := alerts.NewStore(f.store.DB())
	at := time.Now().UTC().Truncate(time.Second)

	grouping := alerts.Grouping{Scope: alerts.ScopeOrganization, Window: 30 * time.Minute}
	for i, device := range devices {
		_, err := store.Record(ctx, alerts.Alert{
			ID:             uuid.New(),
			OrganizationID: organizationID,
			DeviceID:       device,
			RuleID:         "disk-critical",
			RuleVersion:    1,
			Severity:       alerts.SeverityCritical,
			Metric:         "disk.used_percent",
			WindowStart:    at.Add(-time.Duration(i+1) * time.Minute),
			WindowEnd:      at,
			ObservedAt:     at,
		}, grouping)
		require.NoError(t, err)
	}

	incident, found, err := store.OpenIncident(ctx, organizationID, "disk-critical",
		alerts.ScopeOrganization, organizationID)
	require.NoError(t, err)
	require.True(t, found, "the alerts above must have opened a room")
	return incident.ID
}

func incidentCounts(t *testing.T, f *orchestratorFixture, tenantID, incidentID uuid.UUID) (string, int, int) {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), tenantID, true)
	var (
		status                   string
		occurrences, deviceCount int
	)
	require.NoError(t, dbtx.Scoped(ctx, f.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT status, occurrences, device_count FROM incidents WHERE id = $1`, incidentID).
			Scan(&status, &occurrences, &deviceCount)
	}))
	return status, occurrences, deviceCount
}

func TestPurgingADeviceLeavesTheRoomStandingMinusIt(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	tenant := uuid.New()
	doomed := seedDeviceWithTelemetry(t, f, tenant)

	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	organizationID := deviceOrganization(t, f, ctx, doomed)
	site := testutil.SeedSiteIn(t, ctx, f.store, organizationID)
	estate := []uuid.UUID{doomed}
	for range 39 {
		estate = append(estate, testutil.SeedDevice(t, ctx, f.store, site.ID).ID)
	}
	incident := seedInvestigation(t, f, tenant, organizationID, estate)

	job, err := f.orch.PurgeDevice(context.Background(), tenant, doomed, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(context.Background(), job))

	scoped := dbtx.WithTenant(context.Background(), tenant, true)
	assert.Zero(t, countRows(t, f, scoped, qAlerts, doomed),
		"the erased machine's alerts and their evidence go with it")

	status, occurrences, deviceCount := incidentCounts(t, f, tenant, incident)
	assert.Equal(t, 39, deviceCount,
		"the room survives on the other machines, with the erased one removed")
	assert.Equal(t, 39, occurrences)
	assert.Equal(t, "new", status, "a room that still holds alerts stays open")
}

func TestPurgingTheLastDeviceClosesTheRoom(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	tenant := uuid.New()
	only := seedDeviceWithTelemetry(t, f, tenant)

	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	organizationID := deviceOrganization(t, f, ctx, only)
	incident := seedInvestigation(t, f, tenant, organizationID, []uuid.UUID{only})

	job, err := f.orch.PurgeDevice(context.Background(), tenant, only, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(context.Background(), job))

	status, occurrences, deviceCount := incidentCounts(t, f, tenant, incident)
	assert.Equal(t, "resolved", status, "an emptied room is closed rather than left in triage")
	assert.Zero(t, occurrences)
	assert.Zero(t, deviceCount)
}

func TestPurgingATenantLeavesNoInvestigation(t *testing.T) {
	t.Parallel()
	f := newOrchestratorFixture(t)
	tenant := uuid.New()
	device := seedDeviceWithTelemetry(t, f, tenant)

	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	organizationID := deviceOrganization(t, f, ctx, device)
	seedInvestigation(t, f, tenant, organizationID, []uuid.UUID{device})

	job, err := f.orch.PurgeTenant(context.Background(), tenant, nil)
	require.NoError(t, err)
	require.NoError(t, f.orch.Run(context.Background(), job))

	scoped := dbtx.WithTenant(context.Background(), tenant, true)
	for _, query := range []string{qTenantAlerts, qTenantIncidents, qTenantIncidentEvents} {
		var left int
		require.NoError(t, dbtx.Scoped(scoped, f.store.DB(), func(tx *sql.Tx) error {
			return tx.QueryRowContext(scoped, query, tenant).Scan(&left)
		}))
		assert.Zerof(t, left, "a tenant purge must leave nothing behind: %s", query)
	}
}

func deviceOrganization(t *testing.T, f *orchestratorFixture, ctx context.Context, device uuid.UUID) uuid.UUID {
	t.Helper()
	var organizationID uuid.UUID
	require.NoError(t, dbtx.Scoped(ctx, f.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx,
			`SELECT organization_id FROM devices WHERE id = $1`, device).Scan(&organizationID)
	}))
	return organizationID
}

const (
	qAlerts               = `SELECT COUNT(*) FROM alerts WHERE device_id = $1`
	qTenantAlerts         = `SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`
	qTenantIncidents      = `SELECT COUNT(*) FROM incidents WHERE tenant_id = $1`
	qTenantIncidentEvents = `SELECT COUNT(*) FROM incident_events WHERE tenant_id = $1`
)
