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

// tenantPredicate is the tenant clause the tenant-scoped statements name beside the row policy.
// It is spelled out in each statement so every query stays one readable literal.
const tenantPredicate = `tenant_id = current_setting('app.current_tenant')::uuid`

// storeAlertSQL writes one alert with the customer's hourly budget as a condition of the write.
// It returns no row for a spent budget and for a stored identity, which the caller tells apart.
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

// alertByIdentitySQL resolves the identity a reconnect replay carries.
// The identity is the window key, since an agent that lost its store picks fresh alert ids.
const alertByIdentitySQL = `
	SELECT id FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND device_id = $1 AND rule_id = $2 AND rule_version = $3 AND window_start = $4`

// foldIntoStormSQL opens the room a customer's suppressed alerts fold into, or counts one more.
// device_count stays zero because a suppressed alert never became a stored one.
const foldIntoStormSQL = `
	INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
	                       severity, status, opened_at, first_seen, last_seen,
	                       occurrences, device_count)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, 'organization', $3::uuid,
	        $5::text, 'new', $6::timestamptz, $6::timestamptz, $6::timestamptz, 1, 0)
	ON CONFLICT (organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'
	DO UPDATE SET occurrences = incidents.occurrences + 1,
	              last_seen   = EXCLUDED.last_seen`

// openIncidentSQL resolves a grouping key to its open room.
// Grouping keys are guessable, so the tenant predicate resolves another tenant's key to nothing.
const openIncidentSQL = `
	SELECT id, organization_id, rule_id, scope, scope_key, severity, status,
	       assignee_id, opened_at, first_seen, last_seen, resolved_at, cause_code,
	       occurrences, device_count
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1 AND rule_id = $2 AND scope = $3 AND scope_key = $4
	   AND status <> 'resolved'`

// recountRoomsLosingADeviceSQL restates each room the machine fed from the surviving rows.
// It runs before the alerts are deleted, and recomputing keeps a resumed purge idempotent.
const recountRoomsLosingADeviceSQL = `
	UPDATE incidents i
	   SET occurrences  = (SELECT COUNT(*) FROM alerts a
	                        WHERE a.incident_id = i.id AND a.device_id <> $2),
	       device_count = (SELECT COUNT(DISTINCT a.device_id) FROM alerts a
	                        WHERE a.incident_id = i.id AND a.device_id <> $2)
	 WHERE i.tenant_id = $1
	   AND EXISTS (SELECT 1 FROM alerts a WHERE a.incident_id = i.id AND a.device_id = $2)`

// closeEmptiedRoomsSQL closes the rooms the erasure emptied and records why, with no cause code.
// A cause code is a person's answer, and an invented one would feed rule retuning.
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
	// now stamps receipt and bounds the rolling ceiling window, so both share one instant.
	now func() time.Time
}

// NewStore returns a Postgres-backed alert store.
func NewStore(db *sql.DB) *Store {
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
}

// Record files one alert and folds it into its room in one write, reporting Stored, Duplicate or
// CeilingSuppressed. An error means nothing was written.
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
		// The budget is read on the connection about to spend it.
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

		// No row means a stored identity or a spent budget, and only the budget loses an alert.
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

func foldIntoStorm(ctx context.Context, tx *sql.Tx, tenantID, organizationID uuid.UUID, at time.Time) error {
	if _, err := tx.ExecContext(ctx, foldIntoStormSQL,
		uuid.New(), tenantID, organizationID, StormRuleID, string(StormSeverity), at); err != nil {
		return fmt.Errorf("fold suppressed alert into storm incident: %w", err)
	}
	return nil
}

// OpenIncident returns the open room holding a grouping key, and whether there is one.
// A key naming another tenant's room resolves to no room, the same answer as a key naming nothing.
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

// EraseDeviceAlerts repairs the room counts, closes any room the erasure empties, then removes
// one machine's alerts and their evidence. It runs admin-scoped; the tenant predicate confines it.
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

// EraseTenantInvestigations removes a tenant's alerts, rooms and room history by name.
// A tenant purge keeps the tenant row as the audit anchor, so nothing cascades from it.
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
