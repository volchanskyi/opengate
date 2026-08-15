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

// What a purge has to do to an investigation, and the half of it no foreign key
// can do.
//
// Erasing a machine takes its alerts and their evidence with it — that much the
// cascade handles. It cannot touch the counts on the incident those alerts folded
// into, because those are application state: a technician reading "40 machines"
// on a room whose fortieth machine was decommissioned last week is reading a
// number about a machine that no longer exists. Nor can it close a room that ends
// up holding nothing, which would otherwise sit in a customer's triage queue
// forever with no way to shut it.

// seedInvestigation folds one alert per machine into a single room and returns
// the room's id. The devices are the estate the room is about.
func seedInvestigation(
	t *testing.T, f *orchestratorFixture, tenantID, organizationID uuid.UUID, devices []uuid.UUID,
) uuid.UUID {
	t.Helper()
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	store := alerts.NewStore(f.store.DB())
	at := time.Now().UTC().Truncate(time.Second)

	// One estate-wide room, folded by the engine rather than seeded, so what the
	// purge then has to repair is the state a real rollout leaves behind.
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

// incidentCounts reads a room's application state.
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

// TestPurgingADeviceLeavesTheRoomStandingMinusIt drives C8 and E13 end to end,
// through the orchestrator rather than the store: the purge is what a technician
// actually triggers, and the ordering it runs the stages in is the thing that
// could quietly get this wrong.
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

// TestPurgingTheLastDeviceClosesTheRoom is the other half of E13. Left open, an
// emptied room is a line in a triage queue that describes nothing and cannot be
// resolved by anyone, because there is no longer anything to resolve.
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

// TestPurgingATenantLeavesNoInvestigation drives E14's tenant half. A tenant
// purge keeps the tenant row as the anchor for the retained audit trail, so
// nothing cascades from it — the rooms have to be erased by name, or a customer's
// incidents would outlive every machine and every technician they belonged to.
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

// deviceOrganization reads the customer a seeded machine belongs to.
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
