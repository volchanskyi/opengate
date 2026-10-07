package alerts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

func TestOpenInvestigationsCountsWhatIsStillBeingWorked(t *testing.T) {
	e := newEstate(t)

	e.record(t, e.alert(), Stored)
	e.record(t, e.variant(shifted(time.Minute)), Stored)
	room := e.machineRoom(t, "disk-critical")

	byStatus, openAlerts, err := e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{string(StatusNew): 1}, byStatus,
		"one room, and it is in the triage queue")
	assert.Equal(t, 2, openAlerts, "both alerts are in it")

	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	require.NoError(t, e.alerts.Transition(e.ctx, room.ID, Change{To: StatusAcknowledged, Actor: tech}))

	byStatus, openAlerts, err = e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{string(StatusAcknowledged): 1}, byStatus)
	assert.Equal(t, 2, openAlerts)
}

func TestOpenInvestigationsExcludesWhatIsOver(t *testing.T) {
	e := newEstate(t)

	e.record(t, e.alert(), Stored)
	room := e.machineRoom(t, "disk-critical")
	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	require.NoError(t, e.alerts.Transition(e.ctx, room.ID, Change{
		To: StatusResolved, Cause: CauseFixedByTech, Actor: tech,
	}))

	byStatus, openAlerts, err := e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Empty(t, byStatus, "a resolved room is not open in any status")
	assert.Zero(t, openAlerts, "and neither are the alerts it holds")
}

func TestOpenInvestigationsIgnoresAnAlertHoldingNoRoom(t *testing.T) {
	e := newEstate(t)

	e.recordUnder(t, e.variant(func(a *Alert) {
		a.Severity = SeverityInfo
		a.RuleID = "io-stalled"
	}), perCustomer, Stored)

	byStatus, openAlerts, err := e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Empty(t, byStatus, "one observation opens no room")
	assert.Zero(t, openAlerts, "and is not counted as work waiting in one")
}

func TestOpenInvestigationsCountsEveryTenant(t *testing.T) {
	e := newEstate(t)

	e.record(t, e.alert(), Stored)
	neighbour := e.foreignTenant(t)
	e.recordAs(t, neighbour, neighbour.alert(), perMachine, Stored)

	byStatus, openAlerts, err := e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{string(StatusNew): 2}, byStatus,
		"two tenants, one number — the platform's own view is of the whole install")
	assert.Equal(t, 2, openAlerts)
}

func TestOpenInvestigationsNeedsNoCallerScope(t *testing.T) {
	e := newEstate(t)

	e.record(t, e.alert(), Stored)

	_, ok := dbtx.TenantFromContext(context.Background())
	require.False(t, ok, "the case is only meaningful on an unscoped context")

	byStatus, openAlerts, err := e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{string(StatusNew): 1}, byStatus)
	assert.Equal(t, 1, openAlerts)
}

func TestOpenInvestigationsIsOneStatement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, strings.Count(openInvestigationsSQL, "SELECT"),
		"one aggregate, read start to finish")
	assert.NotContains(t, openInvestigationsSQL, "current_setting('app.current_tenant')",
		"the platform's own view is of every tenant, so there is nothing for a predicate to confine it to")
	assert.Contains(t, openInvestigationsSQL, "GROUP BY",
		"the split is the database's work, not a scan the server groups afterwards")
}

func TestOpenStatusesIsTheLifecycleMinusItsEnd(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []Status{StatusNew, StatusAcknowledged, StatusInvestigating}, OpenStatuses())
	assert.NotContains(t, OpenStatuses(), StatusResolved,
		"a room that is over is not open work, which is the whole distinction")
}

func TestOpenInvestigationsSurfacesAReadThatCannotBeAnswered(t *testing.T) {
	t.Parallel()

	blind := NewStore(testutil.NewUnmigratedDB(t))

	byStatus, openAlerts, err := blind.OpenInvestigations(context.Background())
	require.Error(t, err, "a read that cannot reach the tables is a failure, not an empty queue")
	assert.Contains(t, err.Error(), "count open investigations")
	assert.Nil(t, byStatus)
	assert.Zero(t, openAlerts)
}

type foreign struct {
	ctx    context.Context
	tenant uuid.UUID
	org    uuid.UUID
	device uuid.UUID
	now    time.Time
}

func (f foreign) alert() Alert {
	value := 97.1
	return Alert{
		ID:             uuid.New(),
		OrganizationID: f.org,
		DeviceID:       f.device,
		RuleID:         "disk-critical",
		RuleVersion:    3,
		Severity:       SeverityCritical,
		Metric:         "disk.used_percent",
		WindowStart:    f.now.Add(-5 * time.Minute),
		WindowEnd:      f.now,
		ObservedAt:     f.now,
		Value:          &value,
	}
}

func (e estate) foreignTenant(t *testing.T) foreign {
	t.Helper()
	tenantID := uuid.New()
	admin := dbtx.WithDefaultTenant(context.Background(), true)
	testutil.EnsureTenant(t, admin, e.store, tenantID, "Neighbour "+tenantID.String()[:8])

	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	site := testutil.SeedSite(t, ctx, e.store)
	machine := testutil.SeedDevice(t, ctx, e.store, site.ID)
	return foreign{
		ctx:    ctx,
		tenant: tenantID,
		org:    site.OrganizationID,
		device: machine.ID,
		now:    e.now,
	}
}

func (e estate) machineRoom(t *testing.T, ruleID string) Incident {
	t.Helper()
	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, ruleID, ScopeDevice, e.device)
	require.NoError(t, err)
	require.True(t, found, "no open room for %s", ruleID)
	return incident
}

func (e estate) recordAs(t *testing.T, f foreign, a Alert, g Grouping, want Outcome) {
	t.Helper()
	outcome, err := e.alerts.Record(f.ctx, a, g)
	require.NoError(t, err)
	require.Equal(t, want, outcome)
}
