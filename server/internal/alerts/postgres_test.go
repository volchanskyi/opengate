package alerts

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// What the alert store has to guarantee, driven against a real database rather
// than argued from the code.
//
// An alert is the only carrier of the detail behind a signal, so three things
// matter more than the columns. It is written whole or not at all — an alert
// stored beside evidence that failed to write would read as a complete record of
// something nobody can reconstruct. A reconnect replaying a queued alert lands on
// the row it already wrote rather than a second one. And a customer's hourly
// ceiling is a brake on a storm, not a silence: what it refuses is counted and
// folded into one room that says how much was lost.

// The reads every assertion below makes, as static literals so no value is ever
// interpolated into SQL.
const (
	qCustomerAlerts = `SELECT COUNT(*) FROM alerts WHERE organization_id = $1`
	qDeviceAlerts   = `SELECT COUNT(*) FROM alerts WHERE device_id = $1`
	qRoomsForRule   = `SELECT COUNT(*) FROM incidents WHERE organization_id = $1 AND rule_id = $2`
	qRoomState      = `SELECT status, occurrences, device_count FROM incidents WHERE id = $1`
	qRoomCause      = `SELECT cause_code FROM incidents WHERE id = $1`
	qRoomEvent      = `SELECT kind, body FROM incident_events WHERE incident_id = $1`
)

// estate is one customer's seeded rows plus the store under test.
type estate struct {
	store  *db.PostgresStore
	alerts *Store
	ctx    context.Context
	org    uuid.UUID
	device uuid.UUID
	tenant uuid.UUID
	now    time.Time
}

// newEstate seeds a customer with one machine and returns a store whose clock is
// stopped, so a rolling window can be reasoned about exactly.
func newEstate(t *testing.T) estate {
	t.Helper()
	store := testutil.NewTestStore(t)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	site := testutil.SeedSite(t, ctx, store)
	device := testutil.SeedDevice(t, ctx, store, site.ID)

	now := time.Date(2026, 8, 14, 3, 0, 0, 0, time.UTC)
	alerts := NewStore(store.DB())
	alerts.now = func() time.Time { return now }

	return estate{
		store:  store,
		alerts: alerts,
		ctx:    ctx,
		org:    site.OrganizationID,
		device: device.ID,
		tenant: dbtx.DefaultTenantID,
		now:    now,
	}
}

// exec runs one scoped statement, which is how the fixtures below are seeded.
func (e estate) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	require.NoError(t, dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, query, args...)
		return err
	}))
}

// readOne runs one scoped single-row read, which is how every assertion below
// looks at what the store actually wrote — through the same wall production
// reads pass.
func (e estate) readOne(t *testing.T, query string, args []any, dest ...any) {
	t.Helper()
	require.NoError(t, dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(e.ctx, query, args...).Scan(dest...)
	}))
}

func (e estate) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	e.readOne(t, query, args, &n)
	return n
}

// alert is the well-formed alert every case starts from and then changes in one
// place, so a case's name is the only thing that differs from a stored one.
func (e estate) alert() Alert {
	value := 98.2
	return Alert{
		ID:             uuid.New(),
		OrganizationID: e.org,
		DeviceID:       e.device,
		RuleID:         "disk-critical",
		RuleVersion:    3,
		Severity:       SeverityCritical,
		Metric:         "disk.used_percent",
		WindowStart:    e.now.Add(-5 * time.Minute),
		WindowEnd:      e.now,
		ObservedAt:     e.now,
		Evidence:       []byte("compressed-evidence"),
		EvidenceCodec:  "deflate-1",
		Value:          &value,
	}
}

// variant is the well-formed alert with one thing changed and an id of its own,
// which is how a case says "another alert" without restating the whole of one.
func (e estate) variant(change func(*Alert)) Alert {
	a := e.alert()
	a.ID = uuid.New()
	if change != nil {
		change(&a)
	}
	return a
}

// shifted moves an alert's window, which is what makes it a different alert
// rather than a replay of the one before it.
func shifted(by time.Duration) func(*Alert) {
	return func(a *Alert) {
		a.WindowStart = a.WindowStart.Add(by)
		a.WindowEnd = a.WindowEnd.Add(by)
	}
}

// perMachine is how the fixture's rule folds unless a case says otherwise: one
// room per machine, held open a quarter of an hour, which is the shape the
// shipped disk rule declares.
var perMachine = Grouping{Scope: ScopeDevice, Window: 15 * time.Minute}

// perCustomer is the estate-wide shape: one room for the whole customer, held
// open half an hour, which is what a fleet event declares.
var perCustomer = Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute}

// record files one alert and asserts the outcome, which is the single move
// almost every case below is made of.
func (e estate) record(t *testing.T, a Alert, want Outcome) {
	t.Helper()
	e.recordUnder(t, a, perMachine, want)
}

// seedHourOfAlerts writes n alerts stamped as received at receivedAt, which is
// what the hourly ceiling counts. Written directly because the point of the case
// is the ceiling, not the path that filled it.
func (e estate) seedHourOfAlerts(t *testing.T, n int, receivedAt time.Time) {
	t.Helper()
	e.exec(t,
		`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
		                     severity, window_start, window_end, observed_at, received_at)
		 SELECT gen_random_uuid(), $1, $2, $3, 'seeded-load', 1, 'warning',
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz + make_interval(secs => g),
		        $4::timestamptz
		   FROM generate_series(1, $5) AS g`,
		e.tenant, e.org, e.device, receivedAt, n)
}

// room reads an incident's application state — the half no foreign key can keep
// true.
func (e estate) room(t *testing.T, id uuid.UUID) (status string, occurrences, deviceCount int) {
	t.Helper()
	e.readOne(t, qRoomState, []any{id}, &status, &occurrences, &deviceCount)
	return status, occurrences, deviceCount
}

// openRoom resolves a grouping key to the room holding it, failing when there is
// none — every caller here has already established that there should be.
func (e estate) openRoom(t *testing.T, ruleID string) Incident {
	t.Helper()
	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, ruleID, ScopeOrganization, e.org)
	require.NoError(t, err)
	require.True(t, found, "no open room for %s", ruleID)
	return incident
}

// TestAlertAndEvidenceAreWrittenWholeOrNotAtAll drives C1's first half with a
// forced failure rather than by reading the code: evidence past the cap the
// database enforces fails the write, and the alert must not survive it. An alert
// row without the evidence it was raised with would read as a complete record of
// an incident nobody can reconstruct.
func TestAlertAndEvidenceAreWrittenWholeOrNotAtAll(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	_, err := e.alerts.Record(e.ctx, e.variant(func(a *Alert) {
		a.Evidence = make([]byte, MaxEvidenceBytes+1)
	}), perMachine)
	require.Error(t, err, "evidence past the cap must fail the write")
	assert.Zero(t, e.count(t, qCustomerAlerts, e.org),
		"a failed evidence write must leave no alert behind")

	e.record(t, e.variant(func(a *Alert) { a.Evidence = make([]byte, MaxEvidenceBytes) }), Stored)
	assert.Equal(t, 1, e.count(t, qCustomerAlerts, e.org),
		"evidence at the cap is exactly what has to fit")
}

// TestReplayedAlertIsANoOp drives C1's second half and E7. A reconnect replays
// whatever the agent still holds, so the identity — not the id the device chose
// — has to be what a replay resolves against.
func TestReplayedAlertIsANoOp(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	first := e.alert()
	e.record(t, first, Stored)

	// The same alert, re-sent under a new id and a different severity, as an
	// agent that lost its local queue's identity would send it.
	e.record(t, e.variant(func(a *Alert) { a.Severity = SeverityWarning }), Duplicate)
	assert.Equal(t, 1, e.count(t, qCustomerAlerts, e.org), "a replay must not write a second row")

	// The next window is a different alert, not a replay of this one.
	e.record(t, e.variant(shifted(5*time.Minute)), Stored)
	assert.Equal(t, 2, e.count(t, qCustomerAlerts, e.org))
}

// TestCrossTenantReadIsDeniedByACraftedKey drives C7. A grouping key is
// guessable — a rule id is compiled into every build and a customer id travels
// in URLs — so the interesting attack is not a stolen row id but a caller asking
// for a room that exists, in someone else's tenant, by naming it exactly.
func TestCrossTenantReadIsDeniedByACraftedKey(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	tenantB := uuid.New()
	ctxB := dbtx.WithTenant(context.Background(), tenantB, false)
	testutil.EnsureTenant(t, context.Background(), e.store, tenantB, "Tenant "+tenantB.String()[:8])
	siteB := testutil.SeedSite(t, ctxB, e.store)
	deviceB := testutil.SeedDevice(t, ctxB, e.store, siteB.ID)

	e.recordUnder(t, e.alert(), perCustomer, Stored)
	incidentA := e.roomFor(t, perCustomer, "disk-critical", e.org).ID

	// Tenant B, naming tenant A's customer, rule and scope key exactly.
	_, found, err := e.alerts.OpenIncident(ctxB, e.org, "disk-critical", ScopeOrganization, e.org)
	require.NoError(t, err)
	assert.False(t, found, "a crafted grouping key must resolve to not found, never to a row")

	// The same read inside tenant A finds it, so the case above failed for
	// isolation rather than for a key that never matched anything.
	assert.Equal(t, incidentA, e.openRoom(t, "disk-critical").ID)

	// The alert identity is the other guessable key: it is composed entirely of
	// values the endpoint itself chose and could be replayed by anything.
	_, found, err = e.alerts.AlertByIdentity(ctxB, e.device, "disk-critical", 3, e.alert().WindowStart)
	require.NoError(t, err)
	assert.False(t, found, "a crafted alert identity must not read across the wall")

	// A caller with no scope at all fails closed rather than reading as empty.
	_, _, err = e.alerts.OpenIncident(context.Background(), e.org, "disk-critical", ScopeOrganization, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = e.alerts.Record(context.Background(), e.alert(), perMachine)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)

	// Tenant B's own machine keeps working, so the wall is not simply a store
	// that refuses everything.
	own := e.variant(func(a *Alert) {
		a.OrganizationID = siteB.OrganizationID
		a.DeviceID = deviceB.ID
	})
	got, err := e.alerts.Record(ctxB, own, perMachine)
	require.NoError(t, err)
	assert.Equal(t, Stored, got)
}

// TestOrganizationCeilingSuppressesAndFolds drives E9. The ceiling is the
// customer's, never the tenant's: at the tenant one customer's bad night would
// consume the budget of every other customer the MSP looks after, and silencing
// detection across an estate is a worse failure than the storm.
func TestOrganizationCeilingSuppressesAndFolds(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	// A full hour's budget, received 59 minutes ago — still inside a rolling
	// hour, and outside the calendar one an off-by-a-clock implementation would
	// use.
	e.seedHourOfAlerts(t, OrganizationHourlyCeiling, e.now.Add(-59*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)
	assert.Equal(t, OrganizationHourlyCeiling, e.count(t, qCustomerAlerts, e.org),
		"a suppressed alert writes no row")

	// Suppression is never silent: it folds into one room that says how much was
	// lost, and a second suppression joins that room rather than opening another.
	storm := e.openRoom(t, StormRuleID)
	assert.Equal(t, 1, storm.Occurrences)
	assert.Equal(t, e.org, storm.OrganizationID)
	assert.Equal(t, StormRuleID, storm.RuleID)
	assert.Equal(t, ScopeOrganization, storm.Scope)
	assert.Equal(t, e.org, storm.ScopeKey, "the customer whose budget ran out is what the room is about")
	assert.Equal(t, SeverityWarning, storm.Severity)
	assert.Equal(t, StatusNew, storm.Status, "an unclaimed room is the triage queue")
	assert.Equal(t, e.now, storm.FirstSeen.UTC())
	assert.Equal(t, e.now, storm.LastSeen.UTC())
	assert.Zero(t, storm.DeviceCount,
		"a suppressed alert never became a row, so no machine has one in this room")

	e.record(t, e.variant(shifted(time.Minute)), CeilingSuppressed)
	assert.Equal(t, 2, e.openRoom(t, StormRuleID).Occurrences,
		"the count of what was lost is the whole point")
	assert.Equal(t, 1, e.count(t, qRoomsForRule, e.org, StormRuleID),
		"a storm is one room, however long it lasts")
}

// TestCeilingWindowRollsRatherThanResetting pins the window shape. A calendar
// hour would hand a customer a fresh 500 on the stroke of the hour, so a storm
// that started at 02:58 would be un-suppressed two minutes later.
func TestCeilingWindowRollsRatherThanResetting(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, OrganizationHourlyCeiling, e.now.Add(-59*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)

	// Two minutes later the seeded hour has aged past the window, and nothing
	// about the calendar changed to do it.
	later := e.now.Add(2 * time.Minute)
	e.alerts.now = func() time.Time { return later }

	e.record(t, e.variant(shifted(7*time.Minute)), Stored)
}

// TestCeilingIsPerCustomerNotPerTenant proves the choice D28 turns on. Two
// customers of one MSP share a tenant and a database; one spending its budget
// must leave the other's untouched.
func TestCeilingIsPerCustomerNotPerTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, OrganizationHourlyCeiling, e.now.Add(-10*time.Minute))

	// A second customer inside the same tenant, with its own machine.
	otherOrg := testutil.SeedOrganization(t, e.ctx, e.store, "Fabrikam")
	otherSite := testutil.SeedSiteIn(t, e.ctx, e.store, otherOrg)
	otherDevice := testutil.SeedDevice(t, e.ctx, e.store, otherSite.ID)

	e.record(t, e.alert(), CeilingSuppressed)
	e.record(t, e.variant(func(a *Alert) {
		a.OrganizationID = otherOrg
		a.DeviceID = otherDevice.ID
	}), Stored)
}

// TestErasingAMachineRepairsTheRoomItLeaves drives C8 and E13. The foreign key
// takes the alerts and their evidence; it cannot touch the counts on the
// incident, which are application state — so a passing cascade test proves
// nothing about the number a technician actually reads.
func TestErasingAMachineRepairsTheRoomItLeaves(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	// Contoso's rollout: forty machines, one room, folded by the engine rather
	// than seeded, so the numbers under test are the ones a fold produced.
	// DAL-WS-012 is the one being erased; it contributed two of the alerts.
	site := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	for i := range 39 {
		other := testutil.SeedDevice(t, e.ctx, e.store, site.ID)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.DeviceID = other.ID
			shifted(-time.Duration(i+1) * time.Minute)(a)
		}), perCustomer, Stored)
	}
	for i := range 2 {
		e.recordUnder(t, e.variant(shifted(-time.Duration(i+1)*time.Minute)), perCustomer, Stored)
	}
	incident := e.roomFor(t, perCustomer, "disk-critical", e.org).ID
	_, occurrences, deviceCount := e.room(t, incident)
	require.Equal(t, 40, deviceCount, "the fixture is forty machines")
	require.Equal(t, 41, occurrences, "and forty-one alerts")

	require.NoError(t, e.alerts.EraseDeviceAlerts(e.ctx, e.tenant, e.device))

	assert.Zero(t, e.count(t, qDeviceAlerts, e.device),
		"the erased machine's alerts and evidence go with it")
	status, occurrences, deviceCount := e.room(t, incident)
	assert.Equal(t, 39, deviceCount, "the room survives on the other machines, minus this one")
	assert.Equal(t, 39, occurrences, "the erased machine's alerts stop being counted")
	assert.Equal(t, "new", status, "a room that still holds alerts stays open")

	// A second run of the same erasure changes nothing, so a resumed purge is
	// safe to re-run.
	require.NoError(t, e.alerts.EraseDeviceAlerts(e.ctx, e.tenant, e.device))
	_, occurrences, deviceCount = e.room(t, incident)
	assert.Equal(t, 39, deviceCount)
	assert.Equal(t, 39, occurrences)
}

// TestErasingTheLastMachineClosesTheRoom is the other half of E13. A room whose
// every alert has been erased describes nothing, and left open it sits in a
// customer's triage queue forever with no way to close it.
func TestErasingTheLastMachineClosesTheRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	e.recordUnder(t, e.alert(), perCustomer, Stored)
	incident := e.roomFor(t, perCustomer, "disk-critical", e.org).ID

	require.NoError(t, e.alerts.EraseDeviceAlerts(e.ctx, e.tenant, e.device))

	status, occurrences, deviceCount := e.room(t, incident)
	assert.Equal(t, "resolved", status, "an emptied room is closed rather than left in triage")
	assert.Zero(t, occurrences)
	assert.Zero(t, deviceCount)

	var kind string
	var body []byte
	e.readOne(t, qRoomEvent, []any{incident}, &kind, &body)
	assert.Equal(t, "resolution", kind, "why a room closed is part of its history")
	assert.Contains(t, string(body), "erased")

	// Closing is not resolving by hand, so no cause code is invented for it.
	var cause sql.NullString
	e.readOne(t, qRoomCause, []any{incident}, &cause)
	assert.False(t, cause.Valid, "a cause code is a person's answer, not the system's")
}

// TestErasingATenantLeavesNoInvestigation drives E14's tenant half. A tenant
// purge keeps the tenant row as the anchor for the retained audit trail, so
// nothing cascades from it — the investigations have to be erased outright.
func TestErasingATenantLeavesNoInvestigation(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	e.recordUnder(t, e.alert(), perCustomer, Stored)
	incident := e.roomFor(t, perCustomer, "disk-critical", e.org).ID
	e.exec(t,
		`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, kind, body)
		 VALUES ($1, $2, $3, $4, 'comment', '{}'::jsonb)`,
		uuid.New(), e.tenant, e.org, incident)

	require.NoError(t, e.alerts.EraseTenantInvestigations(e.ctx, e.tenant))

	assert.Zero(t, e.count(t, qCustomerAlerts, e.org))
	assert.Zero(t, e.count(t, qRoomsForRule, e.org, "disk-critical"),
		"a tenant purge must leave no incidents behind")
	assert.Zero(t, e.count(t, `SELECT COUNT(*) FROM incident_events WHERE organization_id = $1`, e.org),
		"and none of their history either")
}

// TestUnreachableStoreNeverReportsAnAlertStored drives E19. The edge retries on
// the next reconnect, and that retry is only safe because the server never
// claims to hold something it does not: a store that answered "stored" while
// Postgres was down would lose the alert permanently.
func TestUnreachableStoreNeverReportsAnAlertStored(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	pending := e.alert()

	require.NoError(t, e.store.Close())

	outcome, err := e.alerts.Record(e.ctx, pending, perMachine)
	require.Error(t, err, "an unreachable store is reported, never absorbed")
	assert.NotEqual(t, Stored, outcome)
}

// TestEvidenceCapMatchesTheWire pins the two ends of one number. The agent sizes
// its truncation against the wire's cap; the database refuses anything past this
// one. If they drift, a device would truncate to a size the store then rejects,
// and the alert would be lost carrying exactly the evidence it was told to send.
func TestEvidenceCapMatchesTheWire(t *testing.T) {
	t.Parallel()
	assert.Equal(t, protocol.MaxEvidenceBytes, MaxEvidenceBytes,
		"the stored cap and the wire cap are the same cap")
}

// TestEveryStatementNamesItsTenant is what the shared predicate constant used to
// be. Row-level security is the wall; naming the tenant in the statement too is
// the second lock on the same door, and it is the one that still holds when a
// purge runs admin-scoped in order to act on a tenant it is not.
//
// The statements are written out as whole literals so each can be read start to
// finish rather than assembled from pieces, which is exactly why they need a
// test rather than a constant to keep them honest.
func TestEveryStatementNamesItsTenant(t *testing.T) {
	t.Parallel()
	scoped := map[string]string{
		"storeAlertSQL":                storeAlertSQL,
		"alertByIdentitySQL":           alertByIdentitySQL,
		"openIncidentSQL":              openIncidentSQL,
		"recountRoomsLosingADeviceSQL": recountRoomsLosingADeviceSQL,
		"closeEmptiedRoomsSQL":         closeEmptiedRoomsSQL,
		"deleteDeviceAlertsSQL":        deleteDeviceAlertsSQL,
		"deleteTenantAlertsSQL":        deleteTenantAlertsSQL,
		"deleteTenantIncidentsSQL":     deleteTenantIncidentsSQL,
	}
	for name, query := range scoped {
		assert.Truef(t, strings.Contains(query, tenantPredicate) || strings.Contains(query, "tenant_id = $1"),
			"%s must name the tenant, either from the caller's scope or as an explicit argument", name)
	}

	// The storm room is written under the tenant the alert arrived on, which the
	// policy checks on write; it carries no read predicate of its own.
	assert.Contains(t, foldIntoStormSQL, "tenant_id",
		"a storm room still belongs to exactly one tenant")
}
