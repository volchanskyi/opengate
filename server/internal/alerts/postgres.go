package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The Postgres home of alerts and the rooms they fold into.
//
// Every read and write goes through a tenant-scoped transaction, so row-level
// security is what separates customers rather than a WHERE clause somebody has
// to remember. Each statement also names the tenant itself: the policy is the
// wall, and the predicate is a second lock on the same door — and it is the lock
// that still holds when a purge runs admin-scoped in order to act on a tenant it
// is not.

// tenantPredicate is the second lock, written out in full inside every statement
// below rather than concatenated in from a shared constant. A query assembled
// from pieces is indistinguishable, to anything reading this file, from one
// assembled from input — so each statement here is a single literal that can be
// read start to finish, and TestEveryStatementNamesItsTenant is what keeps the
// predicate on all of them.
const tenantPredicate = `tenant_id = current_setting('app.current_tenant')::uuid`

// storeAlertSQL writes one alert, with the customer's hourly budget as a
// condition of the write rather than a check taken beforehand: the count and the
// insert have to see the same rows or a storm arriving on several connections at
// once would each read a budget that is still free.
//
// It returns no row for two different reasons — a spent budget and an identity
// already stored — which the caller then tells apart. Rolled into one statement
// they would be indistinguishable, and one of them means an alert was lost.
const storeAlertSQL = `
	INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
	                    severity, metric, value, window_start, window_end, observed_at,
	                    received_at, backfilled, evidence, evidence_codec)
	SELECT $1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::text, $6::integer,
	       $7::text, $8::text, $9::double precision, $10::timestamptz, $11::timestamptz,
	       $12::timestamptz, $13::timestamptz, $14::boolean, $15::bytea, $16::text
	 WHERE (SELECT COUNT(*) FROM alerts
	         WHERE tenant_id = current_setting('app.current_tenant')::uuid
	           AND organization_id = $3::uuid
	           AND received_at > $17::timestamptz) < $18::integer
	ON CONFLICT (device_id, rule_id, rule_version, window_start) DO NOTHING
	RETURNING id`

// alertByIdentitySQL resolves the identity a reconnect replay carries. It is
// deliberately not the id the device chose: an agent that lost its local store
// picks a new one and would duplicate every alert it still had to send.
const alertByIdentitySQL = `
	SELECT id FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND device_id = $1 AND rule_id = $2 AND rule_version = $3 AND window_start = $4`

// foldIntoStormSQL opens the room a customer's suppressed alerts fold into, or
// adds one to the count it already carries. Suppression is never silent — what a
// ceiling refuses is detection nobody can reconstruct afterwards, so the number
// lost is the room's whole substance.
//
// device_count stays at zero and means what it says everywhere else: how many
// machines have alerts in this room. A suppressed alert never became one, so
// there are none — the machines still visible are the ones on the alerts that
// were stored before the budget ran out.
const foldIntoStormSQL = `
	INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
	                       severity, status, opened_at, first_seen, last_seen,
	                       occurrences, device_count)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, 'organization', $3::uuid,
	        $5::text, 'new', $6::timestamptz, $6::timestamptz, $6::timestamptz, 1, 0)
	ON CONFLICT (organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'
	DO UPDATE SET occurrences = incidents.occurrences + 1,
	              last_seen   = EXCLUDED.last_seen`

// openIncidentSQL resolves a grouping key to the room holding it, if one is
// open. A grouping key is guessable — a rule id is compiled into every build and
// a customer id travels in URLs — so this read is exactly where a caller would
// try to name someone else's room, and the tenant predicate plus the policy are
// what make that resolve to nothing.
const openIncidentSQL = `
	SELECT id, organization_id, rule_id, scope, scope_key, severity, status,
	       assignee_id, opened_at, first_seen, last_seen, resolved_at, cause_code,
	       occurrences, device_count
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1 AND rule_id = $2 AND scope = $3 AND scope_key = $4
	   AND status <> 'resolved'`

// recountRoomsLosingADeviceSQL restates what is left in every room the machine
// contributed to, from the rows that survive it. Recomputing rather than
// subtracting is what makes a resumed purge safe to run twice.
//
// It runs while the machine's alerts are still there, since afterwards there is
// nothing left to say which rooms it was ever in.
const recountRoomsLosingADeviceSQL = `
	UPDATE incidents i
	   SET occurrences  = (SELECT COUNT(*) FROM alerts a
	                        WHERE a.incident_id = i.id AND a.device_id <> $2),
	       device_count = (SELECT COUNT(DISTINCT a.device_id) FROM alerts a
	                        WHERE a.incident_id = i.id AND a.device_id <> $2)
	 WHERE i.tenant_id = $1
	   AND EXISTS (SELECT 1 FROM alerts a WHERE a.incident_id = i.id AND a.device_id = $2)`

// closeEmptiedRoomsSQL closes the rooms the erasure emptied and records why. A
// room whose every alert has gone describes nothing, and left open it sits in a
// customer's triage queue forever with no way to close it.
//
// No cause code is set. Those are a person's answer to why an incident ended,
// and inventing one here would put a technician's vocabulary in the system's
// mouth — including false_positive, which is the channel that decides whether a
// rule gets retuned.
const closeEmptiedRoomsSQL = `
	WITH closed AS (
	    UPDATE incidents i
	       SET status = 'resolved', resolved_at = $3
	     WHERE i.tenant_id = $1
	       AND i.status <> 'resolved'
	       AND i.occurrences = 0
	       AND EXISTS (SELECT 1 FROM alerts a WHERE a.incident_id = i.id AND a.device_id = $2)
	    RETURNING i.id, i.tenant_id, i.organization_id
	)
	INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, body)
	SELECT gen_random_uuid(), tenant_id, organization_id, id, $3, 'resolution',
	       '{"reason": "last device erased"}'::jsonb
	  FROM closed`

const (
	deleteDeviceAlertsSQL = `DELETE FROM alerts WHERE tenant_id = $1 AND device_id = $2`
	deleteTenantAlertsSQL = `DELETE FROM alerts WHERE tenant_id = $1`
	// Incident events cascade from the incidents they belong to.
	deleteTenantIncidentsSQL = `DELETE FROM incidents WHERE tenant_id = $1`
)

// Store is the Postgres home of a customer's alerts and incidents.
type Store struct {
	db *sql.DB
	// now stamps receipt and bounds the rolling ceiling window. A single clock
	// for both means the budget is measured against the same instant the alert
	// is filed under, rather than against whatever the database thought the time
	// was a round trip later.
	now func() time.Time
}

// NewStore returns a Postgres-backed alert store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// Record files one alert, folds it into the room it belongs to, and reports what
// became of it.
//
// The three outcomes are all ordinary. An alert is stored, or its identity was
// already stored and a reconnect simply replayed it, or the customer's hourly
// budget is spent and the count of what was refused went into the storm room. An
// error means none of those happened — nothing was written, and the caller must
// not report the alert as held.
//
// The alert and its room are one write. An alert stored outside the room it
// belongs to is invisible to the only surface a technician looks at, which is a
// worse failure than the alert never arriving, because nothing says it is
// missing.
func (s *Store) Record(ctx context.Context, a Alert, g Grouping) (Outcome, error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return "", dbtx.ErrTenantRequired
	}
	if err := g.check(); err != nil {
		return "", err
	}
	a = a.normalized()
	now := s.now().UTC().Truncate(time.Microsecond)

	outcome := Stored
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		// The budget is read on the connection that is about to spend it, so an
		// alert is counted against the ceiling in force at that moment rather
		// than one a caller cached before the storm started.
		limits, err := limitsIn(ctx, tx, a.OrganizationID)
		if err != nil {
			return err
		}

		var stored uuid.UUID
		err = tx.QueryRowContext(ctx, storeAlertSQL,
			a.ID, tenant.TenantID, a.OrganizationID, a.DeviceID, a.RuleID, a.RuleVersion,
			string(a.Severity), a.Metric, a.Value, a.WindowStart, a.WindowEnd, a.ObservedAt,
			now, a.Backfilled, a.Evidence, a.EvidenceCodec,
			now.Add(-time.Hour), limits.OrganizationHourly).Scan(&stored)
		switch {
		case err == nil:
			return fold(ctx, tx, tenant.TenantID, a, g, now)
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("store alert: %w", err)
		}

		// Nothing was written. Either this alert is already here, or the budget
		// is spent — one of which means an alert was lost, so they are told
		// apart rather than reported as one thing.
		if _, found, err := identity(ctx, tx, a); err != nil {
			return err
		} else if found {
			outcome = Duplicate
			return nil
		}
		outcome = CeilingSuppressed
		return foldIntoStorm(ctx, tx, tenant.TenantID, a.OrganizationID, now)
	})
	if err != nil {
		return "", err
	}
	return outcome, nil
}

// AlertByIdentity resolves (device, rule, version, window start) to the alert
// stored under it, if any.
func (s *Store) AlertByIdentity(
	ctx context.Context, deviceID uuid.UUID, ruleID string, ruleVersion uint32, windowStart time.Time,
) (uuid.UUID, bool, error) {
	probe := Alert{
		DeviceID: deviceID, RuleID: ruleID, RuleVersion: ruleVersion,
		WindowStart: windowStart,
	}.normalized()

	var (
		id    uuid.UUID
		found bool
	)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		id, found, err = identity(ctx, tx, probe)
		return err
	})
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, found, nil
}

// identity reads the alert stored under a's identity inside an open transaction.
func identity(ctx context.Context, tx *sql.Tx, a Alert) (uuid.UUID, bool, error) {
	var id uuid.UUID
	switch err := tx.QueryRowContext(ctx, alertByIdentitySQL,
		a.DeviceID, a.RuleID, a.RuleVersion, a.WindowStart).Scan(&id); {
	case err == nil:
		return id, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return uuid.Nil, false, nil
	default:
		return uuid.Nil, false, fmt.Errorf("read alert identity: %w", err)
	}
}

// foldIntoStorm records one suppressed alert against the customer's storm room.
func foldIntoStorm(ctx context.Context, tx *sql.Tx, tenantID, organizationID uuid.UUID, at time.Time) error {
	if _, err := tx.ExecContext(ctx, foldIntoStormSQL,
		uuid.New(), tenantID, organizationID, StormRuleID, string(StormSeverity), at); err != nil {
		return fmt.Errorf("fold suppressed alert into storm incident: %w", err)
	}
	return nil
}

// OpenIncident returns the open room holding a grouping key, and whether there
// is one. A key naming another tenant's room resolves to no room at all, which
// is the same answer a key naming nothing gets.
func (s *Store) OpenIncident(
	ctx context.Context, organizationID uuid.UUID, ruleID string, scope Scope, scopeKey uuid.UUID,
) (Incident, bool, error) {
	var (
		incident Incident
		found    bool
	)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		read, err := scanIncident(tx.QueryRowContext(ctx, openIncidentSQL,
			organizationID, ruleID, string(scope), scopeKey))
		switch {
		case err == nil:
			incident, found = read, true
			return nil
		case errors.Is(err, sql.ErrNoRows):
			return nil
		default:
			return fmt.Errorf("read open incident: %w", err)
		}
	})
	if err != nil {
		return Incident{}, false, err
	}
	return incident, found, nil
}

// EraseDeviceAlerts removes one machine's alerts and their evidence, and repairs
// what the foreign key cannot: the counts on the rooms it was in, and a room
// that ends up holding nothing at all.
//
// It runs admin-scoped, like every other stage of a purge — the server acts on a
// tenant it is not. The tenant predicate on each statement is what confines it.
func (s *Store) EraseDeviceAlerts(ctx context.Context, tenantID, deviceID uuid.UUID) error {
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	at := s.now().UTC().Truncate(time.Microsecond)
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, recountRoomsLosingADeviceSQL, tenantID, deviceID); err != nil {
			return fmt.Errorf("recount incidents losing a device: %w", err)
		}
		if _, err := tx.ExecContext(ctx, closeEmptiedRoomsSQL, tenantID, deviceID, at); err != nil {
			return fmt.Errorf("close emptied incidents: %w", err)
		}
		if _, err := tx.ExecContext(ctx, deleteDeviceAlertsSQL, tenantID, deviceID); err != nil {
			return fmt.Errorf("erase device alerts: %w", err)
		}
		return nil
	})
}

// EraseTenantInvestigations removes a tenant's alerts, rooms and room history
// outright. A tenant purge keeps the tenant row as the anchor for the retained
// audit trail, so nothing cascades from it — these have to be erased by name.
func (s *Store) EraseTenantInvestigations(ctx context.Context, tenantID uuid.UUID) error {
	ctx = dbtx.WithTenant(ctx, tenantID, true)
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteTenantAlertsSQL, tenantID); err != nil {
			return fmt.Errorf("erase tenant alerts: %w", err)
		}
		if _, err := tx.ExecContext(ctx, deleteTenantIncidentsSQL, tenantID); err != nil {
			return fmt.Errorf("erase tenant incidents: %w", err)
		}
		return nil
	})
}
