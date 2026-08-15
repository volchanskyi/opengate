package db

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Rehearsal assertions for migration 014: the three tables an investigation is
// made of — the alerts a machine raised, the incidents they fold into, and what
// happened to an incident afterwards.
//
// What matters here is what the database itself refuses. An alert is the only
// carrier of the detail behind a signal and there is no path for asking the
// endpoint again, so the vocabularies these tables store against are closed by
// check constraint rather than by an application convention somebody has to
// remember: a severity nothing downstream can render, a cause code nobody can
// report on, and an evidence blob past the cap are all refused at the database.
// Two open incidents for one grouping key are refused the same way, which is
// what makes folding an alert into an incident race-safe.

// The ids these assertions seed. They sit in their own range so nothing else in
// the rehearsal collides with them.
const (
	invTenantA = "00000000-0000-0000-0000-000000000002"
	invTenantB = "00000000-0000-0000-0000-000000000202"
	invOrgA    = "00000000-0000-0000-0000-000000000601"
	invOrgB    = "00000000-0000-0000-0000-000000000602"
	invDeviceA = "00000000-0000-0000-0000-000000000603"
)

// investigationTables is every table migration 014 creates.
var investigationTables = []string{"alerts", "incidents", "incident_events"}

// assertInvestigationsIntroduced confirms migration 014 built the investigation
// tables with the isolation, the closed vocabularies and the keys they depend on.
func assertInvestigationsIntroduced(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()

	for _, table := range investigationTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Truef(t, reg.Valid, "%s should exist after migration 014", table)

		assert.Equalf(t, 1, policyCount(t, ctx, db, "tenant_isolation_"+table),
			"%s should carry its tenant policy", table)
		assertForcedRowSecurity(t, ctx, db, table)
		assertTenantLeadingIndex(t, ctx, db, table)
	}

	// These assertions seed customers and machines of their own. They run
	// against the migrated database and again against its dump/restore copy,
	// which carries the first run's rows, so they clear their own fixtures on
	// both ends: once so a second run starts clean, once so the rehearsal's
	// shared counts still describe what the migrations built.
	clearInvestigationFixtures(t, ctx, db)
	defer clearInvestigationFixtures(t, ctx, db)

	assertInvestigationsRLSDeniesAcrossTenants(t, ctx, db, schemaName)
	assertClosedVocabulariesRefuseAnythingElse(t, ctx, db)
	assertOneOpenIncidentPerGroupingKey(t, ctx, db)
	assertAlertIdentityIsUnique(t, ctx, db)
	assertEvidenceCapIsEnforcedAtTheDatabase(t, ctx, db)
	assertErasingACustomerLeavesNoInvestigation(t, ctx, db)
}

// clearInvestigationFixtures removes everything the assertions below seed.
// Deleting the two customers is enough: their machines, alerts, incidents and
// incident events all cascade from them.
func clearInvestigationFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`DELETE FROM organizations WHERE id IN ($1, $2)`, invOrgA, invOrgB)
}

// seedInvestigationFixtures inserts one customer per tenant plus a machine in the
// first, which is the least the assertions below need to point at.
func seedInvestigationFixtures(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO organizations (id, tenant_id, name) VALUES ($1, $2, 'Contoso Alerts'), ($3, $4, 'Fabrikam Alerts')
		 ON CONFLICT DO NOTHING`, invOrgA, invTenantA, invOrgB, invTenantB)
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO devices (id, tenant_id, organization_id, hostname) VALUES ($1, $2, $3, 'alerts-fs01')
		 ON CONFLICT DO NOTHING`, invDeviceA, invTenantA, invOrgA)
}

// insertRehearsalAlert writes one alert for the seeded machine, varying only the
// window so each call is its own alert under the identity key.
func insertRehearsalAlert(ctx context.Context, db *sql.DB, severity string, windowStart string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
		                     severity, metric, value, window_start, window_end, observed_at)
		 VALUES ($1, $2, $3, $4, 'disk-critical', 3, $5, 'disk.used_percent', 98.2,
		         $6::timestamptz, $6::timestamptz + INTERVAL '5 minutes', $6::timestamptz)`,
		uuid.New(), invTenantA, invOrgA, invDeviceA, severity, windowStart)
	return err
}

// insertRehearsalIncident opens one incident for the seeded customer.
func insertRehearsalIncident(ctx context.Context, db *sql.DB, status, causeCode string, scopeKey uuid.UUID) error {
	var cause any
	if causeCode != "" {
		cause = causeCode
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen, cause_code)
		 VALUES ($1, $2, $3, 'disk-critical', 'organization', $4, 'warning', $5, NOW(), NOW(), $6)`,
		uuid.New(), invTenantA, invOrgA, scopeKey, status, cause)
	return err
}

// assertInvestigationsRLSDeniesAcrossTenants proves the wall holds for a role
// that is not the table owner: each tenant sees its own incident and nothing
// else, while an admin context sees both.
func assertInvestigationsRLSDeniesAcrossTenants(t *testing.T, ctx context.Context, db *sql.DB, schemaName string) {
	t.Helper()
	const roleName = "opengate_rls_rehearsal"
	ensureRLSRoleInSchema(t, ctx, db, roleName, schemaName)
	seedInvestigationFixtures(t, ctx, db)

	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen)
		 VALUES ($1, $2, $3, 'disk-critical', 'organization', $3, 'warning', 'new', NOW(), NOW()),
		        ($4, $5, $6, 'disk-critical', 'organization', $6, 'warning', 'new', NOW(), NOW())`,
		uuid.New(), invTenantA, invOrgA, uuid.New(), invTenantB, invOrgB)

	txA := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse(invTenantA), false)
	// Rolled back rather than committed: the assertions only read.
	defer func() { _ = txA.Rollback() }()
	var visibleToA int
	require.NoError(t, txA.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents`).Scan(&visibleToA))
	assert.Equal(t, 1, visibleToA, "a tenant should see only its own incidents")

	// The wall also refuses a write into the other tenant's customer, which is
	// the half a read-only test would miss.
	_, err := txA.ExecContext(ctx,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen)
		 VALUES ($1, $2, $3, 'cpu-saturated', 'organization', $3, 'warning', 'new', NOW(), NOW())`,
		uuid.New(), invTenantB, invOrgB)
	assert.Error(t, err, "a tenant must not be able to open an incident for another tenant's customer")

	adminTx := beginTenantTxAsRole(t, ctx, db, roleName, uuid.MustParse(invTenantA), true)
	defer func() { _ = adminTx.Rollback() }()
	var visibleToAdmin int
	require.NoError(t, adminTx.QueryRowContext(ctx, `SELECT COUNT(*) FROM incidents`).Scan(&visibleToAdmin))
	assert.Equal(t, 2, visibleToAdmin)
}

// assertClosedVocabulariesRefuseAnythingElse proves the three closed sets are
// enforced where they cannot be forgotten. A severity nothing can render or a
// cause code nothing can report on would be stored happily by an application
// convention and discovered by whoever opens the incident.
func assertClosedVocabulariesRefuseAnythingElse(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedInvestigationFixtures(t, ctx, db)

	for _, severity := range []string{"info", "warning", "critical"} {
		assert.NoErrorf(t, insertRehearsalAlert(ctx, db, severity, "2026-08-01T00:00:00Z"),
			"%q is one of the three severities", severity)
		rehearsalExecNoTx(t, ctx, db, `DELETE FROM alerts WHERE device_id = $1`, invDeviceA)
	}
	assert.Error(t, insertRehearsalAlert(ctx, db, "Critical", "2026-08-01T00:00:00Z"),
		"the severity vocabulary is the stored spelling, not the wire's")
	assert.Error(t, insertRehearsalAlert(ctx, db, "catastrophic", "2026-08-01T00:00:00Z"),
		"a severity outside the set must be refused at the database")

	assert.NoError(t, insertRehearsalIncident(ctx, db, "resolved", "false_positive", uuid.New()),
		"false_positive is the feedback channel that says a rule needs retuning")
	assert.Error(t, insertRehearsalIncident(ctx, db, "resolved", "gave_up", uuid.New()),
		"a cause code outside the set must be refused at the database")
	assert.Error(t, insertRehearsalIncident(ctx, db, "triaged", "", uuid.New()),
		"a status outside the lifecycle must be refused at the database")

	var incidentID uuid.UUID
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen)
		 VALUES ($1, $2, $3, 'cpu-saturated', 'organization', $3, 'warning', 'new', NOW(), NOW())
		 RETURNING id`, uuid.New(), invTenantA, invOrgA).Scan(&incidentID))

	insertEvent := func(kind string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, kind, body)
			 VALUES ($1, $2, $3, $4, $5, '{}'::jsonb)`,
			uuid.New(), invTenantA, invOrgA, incidentID, kind)
		return err
	}
	for _, kind := range []string{"alert_folded", "status_change", "assignment", "comment", "device_offline", "resolution"} {
		assert.NoErrorf(t, insertEvent(kind), "%q is one of the six things that happen to an incident", kind)
	}
	assert.Error(t, insertEvent("escalation"),
		"an event kind outside the set must be refused at the database")
}

// assertOneOpenIncidentPerGroupingKey proves the partial unique index does the
// refusing. Two open incidents for one grouping key is the race an alert
// arriving on two connections at once would otherwise win, and it would split a
// customer's estate-wide event into two rooms nobody can reconcile.
func assertOneOpenIncidentPerGroupingKey(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedInvestigationFixtures(t, ctx, db)
	scopeKey := uuid.New()

	require.NoError(t, insertRehearsalIncident(ctx, db, "new", "", scopeKey))
	assert.Error(t, insertRehearsalIncident(ctx, db, "acknowledged", "", scopeKey),
		"a second open incident for one grouping key must be refused")

	// Resolved incidents are outside the index, so the same key recurring next
	// month opens a new room rather than colliding with a closed one.
	rehearsalExecNoTx(t, ctx, db,
		`UPDATE incidents SET status = 'resolved', resolved_at = NOW(), cause_code = 'fixed_by_tech'
		  WHERE scope_key = $1`, scopeKey)
	assert.NoError(t, insertRehearsalIncident(ctx, db, "resolved", "resolved_self", scopeKey))
	assert.NoError(t, insertRehearsalIncident(ctx, db, "new", "", scopeKey),
		"the key is free again once every incident holding it is resolved")
}

// assertAlertIdentityIsUnique proves a reconnect replaying a queued alert lands
// on the row it already wrote. The identity is deliberately not the id the
// device chose: an agent that lost its local store would pick a new one and
// duplicate every alert it still had to send.
func assertAlertIdentityIsUnique(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedInvestigationFixtures(t, ctx, db)

	require.NoError(t, insertRehearsalAlert(ctx, db, "critical", "2026-08-02T00:00:00Z"))
	assert.Error(t, insertRehearsalAlert(ctx, db, "critical", "2026-08-02T00:00:00Z"),
		"one (device, rule, version, window start) is one alert")
	assert.NoError(t, insertRehearsalAlert(ctx, db, "critical", "2026-08-02T00:05:00Z"),
		"the next window is a different alert")
}

// assertEvidenceCapIsEnforcedAtTheDatabase proves the cap is a property of the
// row rather than of the path that wrote it. Evidence is immutable and there is
// no path for asking the endpoint again, so a blob that slipped past an
// application check would sit in the table forever.
func assertEvidenceCapIsEnforcedAtTheDatabase(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedInvestigationFixtures(t, ctx, db)

	insertEvidence := func(size int, codec string) error {
		_, err := db.ExecContext(ctx,
			`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
			                     severity, window_start, window_end, observed_at, evidence, evidence_codec)
			 VALUES ($1, $2, $3, $4, 'disk-critical', 7, 'critical',
			         NOW(), NOW(), NOW(), convert_to(repeat('e', $5), 'UTF8'), $6)`,
			uuid.New(), invTenantA, invOrgA, invDeviceA, size, codec)
		return err
	}

	require.NoError(t, insertEvidence(65536, "deflate-1"), "evidence at the cap is exactly what has to fit")
	rehearsalExecNoTx(t, ctx, db, `DELETE FROM alerts WHERE device_id = $1`, invDeviceA)
	assert.Error(t, insertEvidence(65537, "deflate-1"), "evidence past the cap must be refused at the database")
	assert.Error(t, insertEvidence(1024, ""),
		"evidence with no codec named is an unreadable blob, which is worse than none")
}

// assertErasingACustomerLeavesNoInvestigation proves the cascade reaches every
// table a customer's investigations live in. An incident that outlived the
// customer it belongs to is a row nobody can read and nobody can delete.
func assertErasingACustomerLeavesNoInvestigation(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	seedInvestigationFixtures(t, ctx, db)

	var incidentID uuid.UUID
	require.NoError(t, db.QueryRowContext(ctx,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, first_seen, last_seen, occurrences, device_count)
		 VALUES ($1, $2, $3, 'memory-pressure', 'organization', $3, 'critical', 'new', NOW(), NOW(), 1, 1)
		 RETURNING id`, uuid.New(), invTenantA, invOrgA).Scan(&incidentID))
	rehearsalExecNoTx(t, ctx, db,
		`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, kind, body)
		 VALUES ($1, $2, $3, $4, 'alert_folded', '{}'::jsonb)`,
		uuid.New(), invTenantA, invOrgA, incidentID)
	require.NoError(t, insertRehearsalAlert(ctx, db, "critical", "2026-08-03T00:00:00Z"))
	rehearsalExecNoTx(t, ctx, db, `UPDATE alerts SET incident_id = $1 WHERE device_id = $2`, incidentID, invDeviceA)

	rehearsalExecNoTx(t, ctx, db, `DELETE FROM organizations WHERE id = $1`, invOrgA)

	for _, table := range investigationTables {
		var left int
		require.NoError(t, db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM `+sqlIdent(table)+` WHERE organization_id = $1`, invOrgA).Scan(&left))
		assert.Zerof(t, left, "erasing a customer should take its %s with it", table)
	}
}

// assertInvestigationsDownReversal confirms migration 014's down rollback dropped
// the three tables and left the rule tables' shared tenant predicate standing —
// 013 owns it, and taking it down here would break every policy built on it.
func assertInvestigationsDownReversal(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	for _, table := range investigationTables {
		var reg sql.NullString
		require.NoError(t, db.QueryRowContext(ctx, `SELECT to_regclass($1)`, "public."+table).Scan(&reg))
		assert.Falsef(t, reg.Valid, "%s should be gone after the 014 rollback", table)
	}

	var functions int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pg_proc WHERE proname = 'app_tenant_visible'`).Scan(&functions))
	assert.Equal(t, 1, functions, "the shared tenant test belongs to 013 and must survive this rollback")
}
