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

// The reads every assertion below makes, as static literals with no interpolated value.
const (
	qCustomerAlerts = `SELECT COUNT(*) FROM alerts WHERE organization_id = $1`
	qDeviceAlerts   = `SELECT COUNT(*) FROM alerts WHERE device_id = $1`
	qRoomsForRule   = `SELECT COUNT(*) FROM incidents WHERE organization_id = $1 AND rule_id = $2`
	qRoomState      = `SELECT status, occurrences, device_count FROM incidents WHERE id = $1`
	qRoomCause      = `SELECT cause_code FROM incidents WHERE id = $1`
	qRoomEvent      = `SELECT kind, body FROM incident_events WHERE incident_id = $1`
)

type estate struct {
	store  *db.PostgresStore
	alerts *Store
	ctx    context.Context
	org    uuid.UUID
	device uuid.UUID
	tenant uuid.UUID
	now    time.Time
}

// newEstate seeds a customer with one machine and a store whose clock is stopped.
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

func (e estate) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	require.NoError(t, dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(e.ctx, query, args...)
		return err
	}))
}

// readOne runs one single-row read through the same tenant scope production reads use.
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

// alert is the well-formed alert every case starts from and changes in one place.
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

// variant is the well-formed alert with one thing changed and an id of its own.
func (e estate) variant(change func(*Alert)) Alert {
	a := e.alert()
	a.ID = uuid.New()
	if change != nil {
		change(&a)
	}
	return a
}

// shifted moves an alert's window, which makes it a different alert from the one before.
func shifted(by time.Duration) func(*Alert) {
	return func(a *Alert) {
		a.WindowStart = a.WindowStart.Add(by)
		a.WindowEnd = a.WindowEnd.Add(by)
	}
}

// perMachine folds one room per machine, held open a quarter of an hour.
var perMachine = Grouping{Scope: ScopeDevice, Window: 15 * time.Minute}

// perCustomer folds one room for the whole customer, held open half an hour.
var perCustomer = Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute}

func (e estate) record(t *testing.T, a Alert, want Outcome) {
	t.Helper()
	e.recordUnder(t, a, perMachine, want)
}

// seedHourOfAlerts writes n alerts stamped as received at receivedAt, which the hourly
// ceiling counts.
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

// room reads an incident's application state, which no foreign key keeps true.
func (e estate) room(t *testing.T, id uuid.UUID) (status string, occurrences, deviceCount int) {
	t.Helper()
	e.readOne(t, qRoomState, []any{id}, &status, &occurrences, &deviceCount)
	return status, occurrences, deviceCount
}

// openRoom resolves a grouping key to the room holding it and fails when there is none.
func (e estate) openRoom(t *testing.T, ruleID string) Incident {
	t.Helper()
	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, ruleID, ScopeOrganization, e.org)
	require.NoError(t, err)
	require.True(t, found, "no open room for %s", ruleID)
	return incident
}

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

func TestReplayedAlertIsANoOp(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	first := e.alert()
	e.record(t, first, Stored)

	// The same alert re-sent under a new id and a different severity.
	e.record(t, e.variant(func(a *Alert) { a.Severity = SeverityWarning }), Duplicate)
	assert.Equal(t, 1, e.count(t, qCustomerAlerts, e.org), "a replay must not write a second row")

	e.record(t, e.variant(shifted(5*time.Minute)), Stored)
	assert.Equal(t, 2, e.count(t, qCustomerAlerts, e.org))
}

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

	// The same read inside tenant A finds it, so the denial above is isolation.
	assert.Equal(t, incidentA, e.openRoom(t, "disk-critical").ID)

	// The alert identity is composed entirely of values the endpoint chose, so it is guessable.
	_, found, err = e.alerts.AlertByIdentity(ctxB, e.device, "disk-critical", 3, e.alert().WindowStart)
	require.NoError(t, err)
	assert.False(t, found, "a crafted alert identity must not read across the wall")

	// A caller with no scope at all fails closed.
	_, _, err = e.alerts.OpenIncident(context.Background(), e.org, "disk-critical", ScopeOrganization, e.org)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)
	_, err = e.alerts.Record(context.Background(), e.alert(), perMachine)
	assert.ErrorIs(t, err, dbtx.ErrTenantRequired)

	// Tenant B's own machine keeps working, so the denial is isolation, not a refusing store.
	own := e.variant(func(a *Alert) {
		a.OrganizationID = siteB.OrganizationID
		a.DeviceID = deviceB.ID
	})
	got, err := e.alerts.Record(ctxB, own, perMachine)
	require.NoError(t, err)
	assert.Equal(t, Stored, got)
}

func TestOrganizationCeilingSuppressesAndFolds(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	// A full hour's budget received 59 minutes ago is still inside the rolling hour.
	e.seedHourOfAlerts(t, DefaultOrganizationHourlyCeiling, e.now.Add(-59*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)
	assert.Equal(t, DefaultOrganizationHourlyCeiling, e.count(t, qCustomerAlerts, e.org),
		"a suppressed alert writes no row")

	// Suppression folds into one room that counts what was lost; a second joins that room.
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

func TestCeilingWindowRollsRatherThanResetting(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, DefaultOrganizationHourlyCeiling, e.now.Add(-59*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)

	// Two minutes later the seeded hour has aged past the rolling window.
	later := e.now.Add(2 * time.Minute)
	e.alerts.now = func() time.Time { return later }

	e.record(t, e.variant(shifted(7*time.Minute)), Stored)
}

func TestCeilingIsPerCustomerNotPerTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, DefaultOrganizationHourlyCeiling, e.now.Add(-10*time.Minute))

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

func TestErasingAMachineRepairsTheRoomItLeaves(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	// Forty machines fold into one room through the engine; the erased machine adds two alerts.
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

	// A second run of the same erasure changes nothing.
	require.NoError(t, e.alerts.EraseDeviceAlerts(e.ctx, e.tenant, e.device))
	_, occurrences, deviceCount = e.room(t, incident)
	assert.Equal(t, 39, deviceCount)
	assert.Equal(t, 39, occurrences)
}

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

	// A room closed by erasure carries no cause code.
	var cause sql.NullString
	e.readOne(t, qRoomCause, []any{incident}, &cause)
	assert.False(t, cause.Valid, "a cause code is a person's answer, not the system's")
}

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

func TestUnreachableStoreNeverReportsAnAlertStored(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	pending := e.alert()

	require.NoError(t, e.store.Close())

	outcome, err := e.alerts.Record(e.ctx, pending, perMachine)
	require.Error(t, err, "an unreachable store is reported, never absorbed")
	assert.NotEqual(t, Stored, outcome)
}

func TestEvidenceCapMatchesTheWire(t *testing.T) {
	t.Parallel()
	assert.Equal(t, protocol.MaxEvidenceBytes, MaxEvidenceBytes,
		"the stored cap and the wire cap are the same cap")
}

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

	// The storm room is written under the tenant the alert arrived on, which the policy checks.
	assert.Contains(t, foldIntoStormSQL, "tenant_id",
		"a storm room still belongs to exactly one tenant")
}
