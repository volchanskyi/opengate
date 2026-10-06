package alerts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

const (
	qRoomsForRuleAndScope = `SELECT COUNT(*) FROM incidents
	                          WHERE organization_id = $1 AND rule_id = $2 AND scope = $3`
	qRoomOutcome = `SELECT status, cause_code, resolved_at FROM incidents WHERE id = $1`
	qRoomEvents  = `SELECT kind, COALESCE(actor_id::text, ''), body::text
	                   FROM incident_events WHERE incident_id = $1 ORDER BY at, kind`
	qAlertRoom    = `SELECT COALESCE(incident_id::text, '') FROM alerts WHERE id = $1`
	qRoomlessSeen = `SELECT COUNT(*) FROM alerts WHERE organization_id = $1 AND incident_id IS NULL`
)

// rooms counts every room a rule has opened at a scope, closed ones included.
func (e estate) rooms(t *testing.T, ruleID string, scope Scope) int {
	t.Helper()
	return e.count(t, qRoomsForRuleAndScope, e.org, ruleID, string(scope))
}

// at moves an alert to the instant it happened, the event time the fold measures against.
func at(when time.Time) func(*Alert) {
	return func(a *Alert) {
		a.WindowStart, a.WindowEnd, a.ObservedAt = when, when, when
	}
}

type roomEvent struct {
	kind  string
	actor string
	body  string
}

// history reads a room's whole timeline, oldest first.
func (e estate) history(t *testing.T, incidentID uuid.UUID) []roomEvent {
	t.Helper()
	var out []roomEvent
	require.NoError(t, dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(e.ctx, qRoomEvents, incidentID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ev roomEvent
			if err := rows.Scan(&ev.kind, &ev.actor, &ev.body); err != nil {
				return err
			}
			out = append(out, ev)
		}
		return rows.Err()
	}))
	return out
}

// roomOf reads which room an alert landed in, empty when it landed in none.
func (e estate) roomOf(t *testing.T, alertID uuid.UUID) string {
	t.Helper()
	var room string
	e.readOne(t, qAlertRoom, []any{alertID}, &room)
	return room
}

func (e estate) recordUnder(t *testing.T, a Alert, g Grouping, want Outcome) {
	t.Helper()
	got, err := e.alerts.Record(e.ctx, a, g)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// openRoomAt seeds a room in a given state without driving alerts through the fold.
func (e estate) openRoomAt(t *testing.T, status Status, lastSeen time.Time) uuid.UUID {
	t.Helper()
	return e.openRoomIn(t, tenancy{ctx: e.ctx, tenant: e.tenant, org: e.org}, status, lastSeen)
}

// tenancy is one customer inside one tenant, with the scope to read it.
type tenancy struct {
	ctx    context.Context
	tenant uuid.UUID
	org    uuid.UUID
}

func (e estate) openRoomIn(t *testing.T, in tenancy, status Status, lastSeen time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	require.NoError(t, dbtx.Scoped(in.ctx, e.store.DB(), func(tx *sql.Tx) error {
		_, err := tx.ExecContext(in.ctx,
			`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
			                        severity, status, first_seen, last_seen, resolved_at)
			 VALUES ($1::uuid, $2::uuid, $3::uuid, 'disk-critical', 'organization', $3::uuid,
			         'critical', $4::text, $5::timestamptz, $5::timestamptz,
			         CASE WHEN $4::text = 'resolved' THEN $5::timestamptz END)`,
			id, in.tenant, in.org, string(status), lastSeen)
		return err
	}))
	return id
}

func (e estate) outcomeIn(t *testing.T, in tenancy, id uuid.UUID) (status string, cause sql.NullString, resolvedAt sql.NullTime) {
	t.Helper()
	require.NoError(t, dbtx.Scoped(in.ctx, e.store.DB(), func(tx *sql.Tx) error {
		return tx.QueryRowContext(in.ctx, qRoomOutcome, id).Scan(&status, &cause, &resolvedAt)
	}))
	return status, cause, resolvedAt
}

// neighbour seeds a second tenant with a customer and a machine of its own.
func (e estate) neighbour(t *testing.T, name string) tenancy {
	t.Helper()
	tenantID := uuid.New()
	ctx := dbtx.WithTenant(context.Background(), tenantID, false)
	testutil.EnsureTenant(t, context.Background(), e.store, tenantID, name)
	site := testutil.SeedSite(t, ctx, e.store)
	return tenancy{ctx: ctx, tenant: tenantID, org: site.OrganizationID}
}

// sweepAt runs the auto-resolve janitor at an injected instant and asserts the rooms closed.
func (e estate) sweepAt(t *testing.T, at time.Time, windows map[string]time.Duration, want int) {
	t.Helper()
	e.alerts.now = func() time.Time { return at }
	closed, err := e.alerts.ResolveStale(context.Background(), windows)
	require.NoError(t, err)
	assert.Equal(t, want, closed, "rooms closed by the sweep at %s", at)
}

// fleet seeds n more machines for the customer under test and returns their ids.
func (e estate) fleet(t *testing.T, n int) []uuid.UUID {
	t.Helper()
	site := testutil.SeedSiteIn(t, e.ctx, e.store, e.org)
	out := make([]uuid.UUID, 0, n)
	for range n {
		out = append(out, testutil.SeedDevice(t, e.ctx, e.store, site.ID).ID)
	}
	return out
}

func (e estate) roomFor(t *testing.T, g Grouping, ruleID string, scopeKey uuid.UUID) Incident {
	t.Helper()
	incident, found, err := e.alerts.OpenIncident(e.ctx, e.org, ruleID, g.Scope, scopeKey)
	require.NoError(t, err)
	require.Truef(t, found, "no open %s room for %s", g.Scope, ruleID)
	return incident
}

func (e estate) outcome(t *testing.T, id uuid.UUID) (status string, cause sql.NullString, resolvedAt sql.NullTime) {
	t.Helper()
	e.readOne(t, qRoomOutcome, []any{id}, &status, &cause, &resolvedAt)
	return status, cause, resolvedAt
}
