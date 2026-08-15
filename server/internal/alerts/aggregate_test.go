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

// What the fleet-aggregate read has to guarantee.
//
// It answers one question for the platform's own monitoring — how much
// unresolved work the investigation tables are holding — and it answers it about
// every tenant at once, because a triage queue in a tenant nobody happens to be
// serving requests for is still a triage queue. That is the same reason the
// stale-room janitor runs admin-scoped, and it is the property a per-request
// implementation would pass every single-tenant test while getting wrong.

// TestOpenInvestigationsCountsWhatIsStillBeingWorked is the ordinary answer: the
// rooms that are open, split by where each one stands, and the alerts sitting in
// them.
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

	// Moving a room changes which status carries it and nothing else.
	tech := testutil.SeedUser(t, e.ctx, e.store).ID
	require.NoError(t, e.alerts.Transition(e.ctx, room.ID, Change{To: StatusAcknowledged, Actor: tech}))

	byStatus, openAlerts, err = e.alerts.OpenInvestigations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]int{string(StatusAcknowledged): 1}, byStatus)
	assert.Equal(t, 2, openAlerts)
}

// TestOpenInvestigationsExcludesWhatIsOver keeps closed work out of the count. A
// resolved room is not a queue, and its alerts are not waiting for anybody —
// counting them would make the gauge grow with the table rather than with the
// backlog, which is the one thing it exists to distinguish.
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

// TestOpenInvestigationsIgnoresAnAlertHoldingNoRoom keeps a sub-threshold
// observation out of the backlog. It is stored waiting for something to make it
// meaningful and is on nobody's queue until a room opens for it, so counting it
// would report work that does not exist.
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

// TestOpenInvestigationsCountsEveryTenant is why this read is admin-scoped. The
// exported gauge carries no tenant label — it is the platform's own view of the
// whole install — so a room in a tenant this process is not currently serving
// requests for still has to be in the number.
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

// TestOpenInvestigationsNeedsNoCallerScope states the contract the metrics
// updater relies on: it runs on a background goroutine belonging to no request,
// so the read scopes itself rather than requiring a tenant nobody can supply.
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

// TestOpenInvestigationsIsOneStatement is the bound the trap names. These are
// counts over tables that only grow, and the caller refreshes a gauge from them
// on a timer — so the read has to be one aggregate, not one query per status and
// not a second pass for the alerts.
func TestOpenInvestigationsIsOneStatement(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, strings.Count(openInvestigationsSQL, "SELECT"),
		"one aggregate, read start to finish")
	assert.NotContains(t, openInvestigationsSQL, "current_setting('app.current_tenant')",
		"the platform's own view is of every tenant, so there is nothing for a predicate to confine it to")
	assert.Contains(t, openInvestigationsSQL, "GROUP BY",
		"the split is the database's work, not a scan the server groups afterwards")
}

// foreign is a second tenant with its own customer and machine, which is what
// makes "every tenant" a claim rather than a phrase.
type foreign struct {
	ctx    context.Context
	tenant uuid.UUID
	org    uuid.UUID
	device uuid.UUID
	now    time.Time
}

// alert is the neighbour's own well-formed alert.
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

// foreignTenant seeds a whole second tenant: its own customer, its own machine,
// and the scope to write as it.
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

// machineRoom resolves the room a rule's alerts about this estate's one machine
// fold into, failing when there is none — every caller here has already
// established that there should be.
func (e estate) machineRoom(t *testing.T, ruleID string) Incident {
	t.Helper()
	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, ruleID, ScopeDevice, e.device)
	require.NoError(t, err)
	require.True(t, found, "no open room for %s", ruleID)
	return incident
}

// recordAs files an alert as another tenant, which is the only way to put a room
// somewhere this process is not currently serving requests from.
func (e estate) recordAs(t *testing.T, f foreign, a Alert, g Grouping, want Outcome) {
	t.Helper()
	outcome, err := e.alerts.Record(f.ctx, a, g)
	require.NoError(t, err)
	require.Equal(t, want, outcome)
}
