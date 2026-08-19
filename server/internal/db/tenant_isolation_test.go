package db

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tenantFixture holds one tenant's seeded row identifiers. Every tenant table
// gets exactly one row per tenant, so a scoped count is an exact number rather
// than a lower bound.
type tenantFixture struct {
	tenantID     uuid.UUID
	userID       uuid.UUID
	siteID       uuid.UUID
	deviceID     uuid.UUID
	secGroupID   uuid.UUID
	enrollID     uuid.UUID
	orgID        uuid.UUID
	sessionTok   string
	pushEndpoint string
	label        string
}

func newTenantFixture(label string) tenantFixture {
	return tenantFixture{
		tenantID:     uuid.New(),
		userID:       uuid.New(),
		siteID:       uuid.New(),
		deviceID:     uuid.New(),
		secGroupID:   uuid.New(),
		enrollID:     uuid.New(),
		orgID:        uuid.New(),
		sessionTok:   "session-" + uuid.NewString(),
		pushEndpoint: "https://push.example.com/" + uuid.NewString(),
		label:        label,
	}
}

// TestTenantIsolationCoversEveryTenantTable is the behaviour contract the
// tenancy rename must preserve: for every table carrying tenant scope, a caller
// in one tenant sees its own row and none of the other tenant's, and an
// unscoped connection fails closed rather than returning rows.
//
// It is table-driven over the whole tenant-table list so a table added later
// without a policy — or one whose policy is dropped by a migration — fails here
// rather than leaking silently.
func TestTenantIsolationCoversEveryTenantTable(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	a := newTenantFixture("a")
	b := newTenantFixture("b")

	ensureRLSRole(t, ctx, s.db)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO tenants (id, name) VALUES ($1, $2), ($3, $4)`,
		a.tenantID, "Isolation Tenant "+a.tenantID.String(),
		b.tenantID, "Isolation Tenant "+b.tenantID.String())
	require.NoError(t, err)

	seedTenantRows(t, ctx, s.db, a)
	seedTenantRows(t, ctx, s.db, b)

	for _, probe := range tenantIsolationProbes() {
		t.Run(probe.table, func(t *testing.T) {
			assertUnscopedFailsClosed(t, ctx, s.db, probe)
			assertScopedSeesOwnRowOnly(t, ctx, s.db, probe, a, b)
			assertScopedSeesOwnRowOnly(t, ctx, s.db, probe, b, a)
			assertAdminSeesBoth(t, ctx, s.db, probe, a, b)
		})
	}
}

// tenantTablesOutsideTheContract are the tenant-scoped tables deliberately left
// unprobed. Both are erasure bookkeeping — the deny-list and the progress log —
// which must outlive the rows they describe, so they carry the scope column
// without a policy that would hide them from the process doing the erasing.
var tenantTablesOutsideTheContract = []string{"deleted_ids", "purge_jobs"}

// TestEveryTenantTableIsProbed keeps the contract above honest. The probe list
// is written by hand, one static query per table, so this reads the live schema
// and insists the two agree: a table added later with a tenant_id column and no
// probe fails here rather than going unproven, and a probe left behind by a
// dropped table fails too.
func TestEveryTenantTableIsProbed(t *testing.T) {
	s := newPostgresTestStore(t)
	ctx := context.Background()

	inSchema := tenantScopedTableNames(t, ctx, s.db)
	require.NotEmpty(t, inSchema, "the schema query found no tenant tables — the contract has drifted")

	probed := make([]string, 0, len(tenantIsolationProbes()))
	for _, probe := range tenantIsolationProbes() {
		probed = append(probed, probe.table)
	}

	assert.ElementsMatch(t, inSchema, append(probed, tenantTablesOutsideTheContract...),
		"every table carrying tenant_id must either be probed by the isolation contract "+
			"or be named in tenantTablesOutsideTheContract with a reason")
}

// tenantScopedTableNames lists the tables in the live schema that carry a
// tenant_id column.
func tenantScopedTableNames(t *testing.T, ctx context.Context, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(ctx, `
		SELECT c.table_name
		  FROM information_schema.columns c
		  JOIN information_schema.tables t
		    ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		 WHERE c.table_schema = current_schema()
		   AND c.column_name = 'tenant_id'
		   AND t.table_type = 'BASE TABLE'`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var table string
		require.NoError(t, rows.Scan(&table))
		names = append(names, table)
	}
	require.NoError(t, rows.Err())
	return names
}

// tenantIsolationProbe reads one tenant table two ways: everything the caller's
// scope admits, and everything belonging to a named other tenant. Both queries
// are static literals so no table name is ever interpolated into SQL.
type tenantIsolationProbe struct {
	table      string
	countAll   string
	countOther string
}

func tenantIsolationProbes() []tenantIsolationProbe {
	return []tenantIsolationProbe{
		{"organizations",
			`SELECT COUNT(*) FROM organizations`,
			`SELECT COUNT(*) FROM organizations WHERE tenant_id = $1`},
		{"users",
			`SELECT COUNT(*) FROM users`,
			`SELECT COUNT(*) FROM users WHERE tenant_id = $1`},
		{"sites",
			`SELECT COUNT(*) FROM sites`,
			`SELECT COUNT(*) FROM sites WHERE tenant_id = $1`},
		{"devices",
			`SELECT COUNT(*) FROM devices`,
			`SELECT COUNT(*) FROM devices WHERE tenant_id = $1`},
		{"agent_sessions",
			`SELECT COUNT(*) FROM agent_sessions`,
			`SELECT COUNT(*) FROM agent_sessions WHERE tenant_id = $1`},
		{"web_push_subscriptions",
			`SELECT COUNT(*) FROM web_push_subscriptions`,
			`SELECT COUNT(*) FROM web_push_subscriptions WHERE tenant_id = $1`},
		{"audit_events",
			`SELECT COUNT(*) FROM audit_events`,
			`SELECT COUNT(*) FROM audit_events WHERE tenant_id = $1`},
		{"amt_devices",
			`SELECT COUNT(*) FROM amt_devices`,
			`SELECT COUNT(*) FROM amt_devices WHERE tenant_id = $1`},
		{"enrollment_tokens",
			`SELECT COUNT(*) FROM enrollment_tokens`,
			`SELECT COUNT(*) FROM enrollment_tokens WHERE tenant_id = $1`},
		{"security_groups",
			`SELECT COUNT(*) FROM security_groups`,
			`SELECT COUNT(*) FROM security_groups WHERE tenant_id = $1`},
		{"security_group_members",
			`SELECT COUNT(*) FROM security_group_members`,
			`SELECT COUNT(*) FROM security_group_members WHERE tenant_id = $1`},
		{"device_updates",
			`SELECT COUNT(*) FROM device_updates`,
			`SELECT COUNT(*) FROM device_updates WHERE tenant_id = $1`},
		{"device_hardware",
			`SELECT COUNT(*) FROM device_hardware`,
			`SELECT COUNT(*) FROM device_hardware WHERE tenant_id = $1`},
		{"device_processes",
			`SELECT COUNT(*) FROM device_processes`,
			`SELECT COUNT(*) FROM device_processes WHERE tenant_id = $1`},
		{"device_inventory",
			`SELECT COUNT(*) FROM device_inventory`,
			`SELECT COUNT(*) FROM device_inventory WHERE tenant_id = $1`},
		{"rule_bindings",
			`SELECT COUNT(*) FROM rule_bindings`,
			`SELECT COUNT(*) FROM rule_bindings WHERE tenant_id = $1`},
		{"rule_rollout",
			`SELECT COUNT(*) FROM rule_rollout`,
			`SELECT COUNT(*) FROM rule_rollout WHERE tenant_id = $1`},
		{"rule_coverage_unsupported",
			`SELECT COUNT(*) FROM rule_coverage_unsupported`,
			`SELECT COUNT(*) FROM rule_coverage_unsupported WHERE tenant_id = $1`},
		{"alerts",
			`SELECT COUNT(*) FROM alerts`,
			`SELECT COUNT(*) FROM alerts WHERE tenant_id = $1`},
		{"incidents",
			`SELECT COUNT(*) FROM incidents`,
			`SELECT COUNT(*) FROM incidents WHERE tenant_id = $1`},
		{"incident_events",
			`SELECT COUNT(*) FROM incident_events`,
			`SELECT COUNT(*) FROM incident_events WHERE tenant_id = $1`},
		{"device_tag_labels",
			`SELECT COUNT(*) FROM device_tag_labels`,
			`SELECT COUNT(*) FROM device_tag_labels WHERE tenant_id = $1`},
		{"device_tags",
			`SELECT COUNT(*) FROM device_tags`,
			`SELECT COUNT(*) FROM device_tags WHERE tenant_id = $1`},
		{"organization_alert_limits",
			`SELECT COUNT(*) FROM organization_alert_limits`,
			`SELECT COUNT(*) FROM organization_alert_limits WHERE tenant_id = $1`},
		{"rule_binding_clamps",
			`SELECT COUNT(*) FROM rule_binding_clamps`,
			`SELECT COUNT(*) FROM rule_binding_clamps WHERE tenant_id = $1`},
	}
}

// assertUnscopedFailsClosed proves the policies have no missing_ok fallback: a
// connection that never set a tenant errors instead of returning an empty set,
// so a forgotten scope can never read as "this tenant has nothing".
func assertUnscopedFailsClosed(t *testing.T, ctx context.Context, db *sql.DB, probe tenantIsolationProbe) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback() //nolint:errcheck // read-only probe
	_, err = tx.ExecContext(ctx, `SET LOCAL ROLE opengate_rls_test`)
	require.NoError(t, err)

	var count int
	err = tx.QueryRowContext(ctx, probe.countAll).Scan(&count)
	require.Error(t, err, "%s must fail closed without tenant scope", probe.table)
}

// assertScopedSeesOwnRowOnly proves the read boundary in both directions: the
// caller's own seeded row is visible and the other tenant's is not.
func assertScopedSeesOwnRowOnly(t *testing.T, ctx context.Context, db *sql.DB, probe tenantIsolationProbe, own, other tenantFixture) {
	t.Helper()
	tx := beginTenantTx(t, ctx, db, own.tenantID, false)
	defer tx.Rollback() //nolint:errcheck // read-only probe

	var visible int
	require.NoError(t, tx.QueryRowContext(ctx, probe.countAll).Scan(&visible))
	assert.Equal(t, 1, visible, "%s: tenant %s must see exactly its own row", probe.table, own.label)

	var leaked int
	require.NoError(t, tx.QueryRowContext(ctx, probe.countOther, other.tenantID).Scan(&leaked))
	assert.Zero(t, leaked, "%s: tenant %s must not read tenant %s", probe.table, own.label, other.label)
}

// assertAdminSeesBoth proves the deliberate cross-tenant path still works, so a
// tightened policy that broke server-side purge or reconciliation is caught here
// rather than in production.
func assertAdminSeesBoth(t *testing.T, ctx context.Context, db *sql.DB, probe tenantIsolationProbe, a, b tenantFixture) {
	t.Helper()
	tx := beginTenantTx(t, ctx, db, a.tenantID, true)
	defer tx.Rollback() //nolint:errcheck // read-only probe

	var fromA, fromB int
	require.NoError(t, tx.QueryRowContext(ctx, probe.countOther, a.tenantID).Scan(&fromA))
	require.NoError(t, tx.QueryRowContext(ctx, probe.countOther, b.tenantID).Scan(&fromB))
	assert.Equal(t, 1, fromA, "%s: admin scope must read tenant a", probe.table)
	assert.Equal(t, 1, fromB, "%s: admin scope must read tenant b", probe.table)
}

// seedTenantRows writes exactly one row per tenant table for f, inside f's own
// tenant scope — so the WITH CHECK half of every policy is exercised too.
func seedTenantRows(t *testing.T, ctx context.Context, db *sql.DB, f tenantFixture) {
	t.Helper()
	now := time.Now().UTC()

	tx := beginTenantTx(t, ctx, db, f.tenantID, false)
	defer tx.Rollback() //nolint:errcheck // harmless after Commit

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.ExecContext(ctx, query, args...)
		require.NoError(t, err, "seed %s", f.label)
	}

	exec(`INSERT INTO users (id, tenant_id, email, password_hash) VALUES ($1, $2, $3, 'hash')`,
		f.userID, f.tenantID, "isolation-"+f.userID.String()+"@example.com")
	exec(`INSERT INTO organizations (id, tenant_id, name) VALUES ($1, $2, $3)`,
		f.orgID, f.tenantID, "Customer "+f.orgID.String())
	exec(`INSERT INTO sites (id, tenant_id, organization_id, name) VALUES ($1, $2, $3, 'isolation site')`,
		f.siteID, f.tenantID, f.orgID)
	exec(`INSERT INTO devices (id, tenant_id, organization_id, site_id, hostname) VALUES ($1, $2, $3, $4, $5)`,
		f.deviceID, f.tenantID, f.orgID, f.siteID, "host-"+f.label)
	exec(`INSERT INTO agent_sessions (token, tenant_id, device_id, user_id) VALUES ($1, $2, $3, $4)`,
		f.sessionTok, f.tenantID, f.deviceID, f.userID)
	exec(`INSERT INTO web_push_subscriptions (endpoint, tenant_id, user_id) VALUES ($1, $2, $3)`,
		f.pushEndpoint, f.tenantID, f.userID)
	exec(`INSERT INTO audit_events (tenant_id, user_id, action) VALUES ($1, $2, 'isolation.probe')`,
		f.tenantID, f.userID)
	exec(`INSERT INTO amt_devices (uuid, tenant_id, device_id) VALUES ($1, $2, $3)`,
		uuid.New(), f.tenantID, f.deviceID)
	exec(`INSERT INTO enrollment_tokens (id, tenant_id, token, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		f.enrollID, f.tenantID, "enroll-"+f.enrollID.String(), f.userID, now.Add(time.Hour))
	exec(`INSERT INTO security_groups (id, tenant_id, name) VALUES ($1, $2, $3)`,
		f.secGroupID, f.tenantID, "isolation-sg-"+f.secGroupID.String())
	exec(`INSERT INTO security_group_members (group_id, tenant_id, user_id) VALUES ($1, $2, $3)`,
		f.secGroupID, f.tenantID, f.userID)
	exec(`INSERT INTO device_updates (tenant_id, device_id, version) VALUES ($1, $2, '1.0.0')`,
		f.tenantID, f.deviceID)
	exec(`INSERT INTO device_hardware (device_id, tenant_id) VALUES ($1, $2)`,
		f.deviceID, f.tenantID)
	exec(`INSERT INTO device_processes (tenant_id, device_id, ts, rank, pid) VALUES ($1, $2, $3, 0, 1)`,
		f.tenantID, f.deviceID, now)
	exec(`INSERT INTO device_inventory (tenant_id, device_id, kind, name, first_seen, last_seen) VALUES ($1, $2, 'port', 'isolation', $3, $4)`,
		f.tenantID, f.deviceID, now, now)

	bindingID := uuid.New()
	exec(`INSERT INTO rule_bindings (id, tenant_id, organization_id, rule_id, level, level_key, params)
	      VALUES ($1, $2, $3, 'disk-critical', 'organization', $3, '{"threshold": 95}'::jsonb)`,
		bindingID, f.tenantID, f.orgID)
	exec(`INSERT INTO rule_rollout (tenant_id, organization_id, rule_id) VALUES ($1, $2, 'disk-critical')`,
		f.tenantID, f.orgID)
	exec(`INSERT INTO rule_coverage_unsupported (tenant_id, organization_id, device_id, rule_id)
	      VALUES ($1, $2, $3, 'io-stalled')`,
		f.tenantID, f.orgID, f.deviceID)

	labelID := uuid.New()
	exec(`INSERT INTO device_tag_labels (id, tenant_id, organization_id, key, value)
	      VALUES ($1, $2, $3, 'role', 'file-server')`,
		labelID, f.tenantID, f.orgID)
	exec(`INSERT INTO device_tags (tenant_id, organization_id, device_id, label_id, key, value)
	      VALUES ($1, $2, $3, $4, 'role', 'file-server')`,
		f.tenantID, f.orgID, f.deviceID, labelID)
	exec(`INSERT INTO organization_alert_limits
	          (tenant_id, organization_id, hourly_ceiling, device_hourly_ceiling)
	      VALUES ($1, $2, 500, 20)`,
		f.tenantID, f.orgID)

	exec(`INSERT INTO rule_binding_clamps
	          (id, tenant_id, organization_id, binding_id, rule_id, rule_version,
	           param, from_value, to_value)
	      VALUES ($1, $2, $3, $4, 'disk-critical', 2, 'threshold', 95, 90)`,
		uuid.New(), f.tenantID, f.orgID, bindingID)

	incidentID := uuid.New()
	exec(`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
	                             severity, status, first_seen, last_seen)
	      VALUES ($1, $2, $3, 'disk-critical', 'organization', $3, 'warning', 'new', $4, $4)`,
		incidentID, f.tenantID, f.orgID, now)
	exec(`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
	                          severity, metric, value, window_start, window_end, observed_at, incident_id)
	      VALUES ($1, $2, $3, $4, 'disk-critical', 1, 'critical', 'disk.used_percent', 98.2, $5, $5, $5, $6)`,
		uuid.New(), f.tenantID, f.orgID, f.deviceID, now, incidentID)
	exec(`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, kind, at, body)
	      VALUES ($1, $2, $3, $4, 'alert_folded', $5, '{}'::jsonb)`,
		uuid.New(), f.tenantID, f.orgID, incidentID, now)

	require.NoError(t, tx.Commit())
}
