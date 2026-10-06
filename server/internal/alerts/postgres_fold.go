package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// The row lock serialises concurrent folds into one open room.
const lockOpenRoomSQL = `
	SELECT id, first_seen, last_seen
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1 AND rule_id = $2 AND scope = $3 AND scope_key = $4
	   AND status <> 'resolved'
	   FOR UPDATE`

// Closes at the instant the room became closeable and leaves the cause code to a person.
const closeLapsedRoomSQL = `
	WITH closed AS (
	    UPDATE incidents
	       SET status = 'resolved', resolved_at = $2
	     WHERE tenant_id = current_setting('app.current_tenant')::uuid
	       AND id = $1 AND status <> 'resolved'
	    RETURNING id, tenant_id, organization_id
	)
	INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, body)
	SELECT gen_random_uuid(), tenant_id, organization_id, id, $2, 'resolution',
	       '{"reason": "no alert within the reopen window"}'::jsonb
	  FROM closed`

// The conflict clause makes concurrent folds converge on one room.
const openOrJoinRoomSQL = `
	INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
	                       severity, status, opened_at, first_seen, last_seen,
	                       occurrences, device_count)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::text, $6::uuid,
	        $7::text, 'new', $8::timestamptz, $9::timestamptz, $9::timestamptz, 0, 0)
	ON CONFLICT (organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'
	DO UPDATE SET last_seen = GREATEST(incidents.last_seen, EXCLUDED.last_seen)
	RETURNING id`

const attachAlertSQL = `
	UPDATE alerts SET incident_id = $1
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $2`

// The site rung is read from the machine, since an alert row carries no site.
const attachPendingObservationsSQL = `
	UPDATE alerts a SET incident_id = $1
	 WHERE a.tenant_id = current_setting('app.current_tenant')::uuid
	   AND a.incident_id IS NULL
	   AND a.organization_id = $2 AND a.rule_id = $3 AND a.severity = 'info'
	   AND a.observed_at BETWEEN $4::timestamptz AND $5::timestamptz
	   AND (   $6::text = 'organization'
	        OR ($6::text = 'device' AND a.device_id = $7::uuid)
	        OR ($6::text = 'site' AND EXISTS (
	                SELECT 1 FROM devices d WHERE d.id = a.device_id AND d.site_id = $7::uuid)))`

const countPendingObserversSQL = `
	SELECT COUNT(DISTINCT a.device_id)
	  FROM alerts a
	 WHERE a.tenant_id = current_setting('app.current_tenant')::uuid
	   AND a.incident_id IS NULL
	   AND a.organization_id = $1 AND a.rule_id = $2 AND a.severity = 'info'
	   AND a.observed_at BETWEEN $3::timestamptz AND $4::timestamptz
	   AND (   $5::text = 'organization'
	        OR ($5::text = 'device' AND a.device_id = $6::uuid)
	        OR ($5::text = 'site' AND EXISTS (
	                SELECT 1 FROM devices d WHERE d.id = a.device_id AND d.site_id = $6::uuid)))`

// Restating from the alerts survives concurrent folds and erasures.
const restateRoomFromItsAlertsSQL = `
	UPDATE incidents i
	   SET occurrences  = held.alerts,
	       device_count = held.machines,
	       first_seen   = held.first_seen,
	       last_seen    = held.last_seen,
	       severity     = held.severity
	  FROM (SELECT COUNT(*)                    AS alerts,
	               COUNT(DISTINCT a.device_id) AS machines,
	               MIN(a.observed_at)          AS first_seen,
	               MAX(a.observed_at)          AS last_seen,
	               CASE WHEN bool_or(a.severity = 'critical') THEN 'critical'
	                    WHEN bool_or(a.severity = 'warning')  THEN 'warning'
	                    ELSE 'info' END        AS severity
	          FROM alerts a WHERE a.incident_id = $1) AS held
	 WHERE i.tenant_id = current_setting('app.current_tenant')::uuid
	   AND i.id = $1 AND held.alerts > 0`

// The grouping key derives from the machine's own place in the ladder, never from the endpoint.
const deviceSiteSQL = `
	SELECT site_id FROM devices
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

type openRoom struct {
	id        uuid.UUID
	firstSeen time.Time
	lastSeen  time.Time
}

type folding struct {
	tx       *sql.Tx
	tenantID uuid.UUID
	alert    Alert
	grouping Grouping
	key      groupingKey
	now      time.Time
}

func fold(ctx context.Context, tx *sql.Tx, tenantID uuid.UUID, a Alert, g Grouping, now time.Time) error {
	key, err := scopeKeyFor(ctx, tx, a, g.Scope)
	if err != nil {
		return err
	}
	f := folding{tx: tx, tenantID: tenantID, alert: a, grouping: g, key: key, now: now}

	room, held, err := f.lockOpenRoom(ctx)
	if err != nil {
		return err
	}
	switch {
	case held && g.spans(room.firstSeen, room.lastSeen, a.ObservedAt):
		return f.fileInto(ctx, room.id)
	case held && g.lapsed(room.lastSeen, a.ObservedAt):
		if err := f.closeLapsed(ctx, room); err != nil {
			return err
		}
	case held:
		// An alert predating a live room stays unfiled.
		return nil
	}

	opens, err := f.opensARoom(ctx)
	if err != nil || !opens {
		return err
	}
	return f.openRoom(ctx)
}

// A machine filed into no site narrows to a device room.
func scopeKeyFor(ctx context.Context, tx *sql.Tx, a Alert, scope Scope) (groupingKey, error) {
	key := groupingKey{organizationID: a.OrganizationID, ruleID: a.RuleID, scope: scope}
	switch scope {
	case ScopeOrganization:
		key.scopeKey = a.OrganizationID
		return key, nil
	case ScopeDevice:
		key.scopeKey = a.DeviceID
		return key, nil
	case ScopeSite:
		var siteID uuid.NullUUID
		switch err := tx.QueryRowContext(ctx, deviceSiteSQL, a.DeviceID).Scan(&siteID); {
		case errors.Is(err, sql.ErrNoRows):
			return groupingKey{}, fmt.Errorf("derive grouping key: no machine %s", a.DeviceID)
		case err != nil:
			return groupingKey{}, fmt.Errorf("derive grouping key: %w", err)
		case !siteID.Valid:
			key.scope, key.scopeKey = ScopeDevice, a.DeviceID
		default:
			key.scopeKey = siteID.UUID
		}
		return key, nil
	default:
		return groupingKey{}, fmt.Errorf("%w: scope %q", ErrGroupingUnusable, scope)
	}
}

func (f folding) lockOpenRoom(ctx context.Context) (openRoom, bool, error) {
	return lockOpenRoomForKey(ctx, f.tx, f.key)
}

func lockOpenRoomForKey(ctx context.Context, tx *sql.Tx, key groupingKey) (openRoom, bool, error) {
	var room openRoom
	switch err := tx.QueryRowContext(ctx, lockOpenRoomSQL,
		key.organizationID, key.ruleID, string(key.scope), key.scopeKey).
		Scan(&room.id, &room.firstSeen, &room.lastSeen); {
	case err == nil:
		return room, true, nil
	case errors.Is(err, sql.ErrNoRows):
		return openRoom{}, false, nil
	default:
		return openRoom{}, false, fmt.Errorf("read open incident for grouping key: %w", err)
	}
}

// A lapsed room closes at its last alert plus the window, the instant it became closeable.
func (f folding) closeLapsed(ctx context.Context, room openRoom) error {
	at := room.lastSeen.Add(f.grouping.Window)
	if _, err := f.tx.ExecContext(ctx, closeLapsedRoomSQL, room.id, at); err != nil {
		return fmt.Errorf("close lapsed incident: %w", err)
	}
	return nil
}

// An info observation waits unfiled until a second machine reports it inside the window.
func (f folding) opensARoom(ctx context.Context) (bool, error) {
	if f.alert.Severity != SeverityInfo {
		return true, nil
	}
	if f.key.scope == ScopeDevice {
		// Co-occurrence needs two machines, so a device-scoped observation never raises a room.
		return false, nil
	}
	from, to := f.coOccurrenceWindow()
	var observers int
	if err := f.tx.QueryRowContext(ctx, countPendingObserversSQL,
		f.key.organizationID, f.key.ruleID, from, to,
		string(f.key.scope), f.key.scopeKey).Scan(&observers); err != nil {
		return false, fmt.Errorf("count co-occurring observations: %w", err)
	}
	return observers > 1, nil
}

func (f folding) openRoom(ctx context.Context) error {
	var id uuid.UUID
	if err := f.tx.QueryRowContext(ctx, openOrJoinRoomSQL,
		uuid.New(), f.tenantID, f.key.organizationID, f.key.ruleID,
		string(f.key.scope), f.key.scopeKey,
		string(f.alert.Severity), f.now, f.alert.ObservedAt).Scan(&id); err != nil {
		return fmt.Errorf("open incident: %w", err)
	}
	from, to := f.coOccurrenceWindow()
	if _, err := f.tx.ExecContext(ctx, attachPendingObservationsSQL, id,
		f.key.organizationID, f.key.ruleID, from, to,
		string(f.key.scope), f.key.scopeKey); err != nil {
		return fmt.Errorf("gather pending observations: %w", err)
	}
	return f.fileInto(ctx, id)
}

func (f folding) coOccurrenceWindow() (from, to time.Time) {
	return f.alert.ObservedAt.Add(-f.grouping.Window), f.alert.ObservedAt.Add(f.grouping.Window)
}

func (f folding) fileInto(ctx context.Context, id uuid.UUID) error {
	if _, err := f.tx.ExecContext(ctx, attachAlertSQL, id, f.alert.ID); err != nil {
		return fmt.Errorf("file alert into incident: %w", err)
	}
	if _, err := f.tx.ExecContext(ctx, restateRoomFromItsAlertsSQL, id); err != nil {
		return fmt.Errorf("restate incident from its alerts: %w", err)
	}
	return nil
}

type groupingKey struct {
	organizationID uuid.UUID
	ruleID         string
	scope          Scope
	scopeKey       uuid.UUID
}
