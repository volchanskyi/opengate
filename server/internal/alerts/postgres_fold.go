package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Folding an alert into the room it belongs to.
//
// Grouping is the whole value of an incident: 312 alerts a technician cannot
// read are one room they can. Two axes decide which room — how wide it is, and
// how long firings on one key stay one thing — and this file is where an
// arriving alert is measured against both.
//
// Every statement is a single literal that can be read start to finish, for the
// reason stated in postgres.go: a query assembled from pieces is
// indistinguishable, to anything reading this file, from one assembled from
// input. TestEveryIncidentStatementNamesItsTenantExceptTheJanitor is what keeps
// the tenant predicate on all of them.

// lockOpenRoomSQL reads the open room holding a grouping key and holds it for
// the rest of the transaction. The lock is what serialises two alerts arriving
// on two connections for one estate-wide event: without it both would read "no
// room", and only one of the two rooms they opened would survive the index.
const lockOpenRoomSQL = `
	SELECT id, first_seen, last_seen
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1 AND rule_id = $2 AND scope = $3 AND scope_key = $4
	   AND status <> 'resolved'
	   FOR UPDATE`

// closeLapsedRoomSQL closes a room the arriving alert is too late to join, and
// says why in the room's own history.
//
// It closes the room at the instant it *became* closeable rather than at the
// moment somebody noticed, so the sweep and the fold record the same time for
// the same room however long the gap between them was. No cause code is set:
// those are a person's answer, and inventing one here would put a technician's
// vocabulary in the system's mouth.
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

// openOrJoinRoomSQL opens the room for a grouping key, or hands back the one
// that already holds it.
//
// The conflict clause is what makes concurrent folds converge: an insert that
// loses the race blocks on the winner's row rather than failing, so both alerts
// end up in one room instead of one of them erroring out. The counts stay at
// zero here — they are restated from the room's own alerts afterwards, so a
// fold and an erasure arrive at the same number by the same route.
const openOrJoinRoomSQL = `
	INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
	                       severity, status, opened_at, first_seen, last_seen,
	                       occurrences, device_count)
	VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, $5::text, $6::uuid,
	        $7::text, 'new', $8::timestamptz, $9::timestamptz, $9::timestamptz, 0, 0)
	ON CONFLICT (organization_id, rule_id, scope, scope_key) WHERE status <> 'resolved'
	DO UPDATE SET last_seen = GREATEST(incidents.last_seen, EXCLUDED.last_seen)
	RETURNING id`

// attachAlertSQL files one alert into the room it folded into.
const attachAlertSQL = `
	UPDATE alerts SET incident_id = $1
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $2`

// attachPendingObservationsSQL files the readings that were waiting into the
// room they turned out to belong to.
//
// A low-severity observation is not an incident on its own — one host a little
// slower than usual is noise — so it is stored holding no room until something
// makes it meaningful. When a room for its key does open, those readings are the
// context the investigation wants, and leaving them loose would hide the shape
// of the event from the only surface anybody looks at.
//
// The site rung is read from the machine rather than the alert on purpose: which
// office a machine is filed into is a fact about the machine and can change, and
// a copy on the alert row would make a year-old alert claim a site the machine
// has since left.
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

// countPendingObserversSQL counts how many distinct machines are reporting the
// same sub-threshold observation, which is the only thing that turns one into an
// incident. A fleet event where no host individually breaches is visible exactly
// because several hosts see it at once.
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

// restateRoomFromItsAlertsSQL rewrites everything the room says about itself
// from the alerts it actually holds.
//
// Restating rather than incrementing is what makes the numbers survive the two
// things that break a counter: a concurrent fold, where two increments can read
// the same starting value, and an erasure, where a machine's rows leave and no
// foreign key can subtract them. It is also why a resumed purge is safe to run
// twice. `occurrences` counts alerts and `device_count` counts machines — forty
// machines and 312 alerts are the same event and two very different numbers.
//
// The span is event time throughout, so a retroactive finding places the room
// where it happened instead of where it was received.
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

// deviceSiteSQL reads which office a machine is filed into. A grouping key is
// derived on this side from the machine's own place in the tenancy ladder,
// never taken from the endpoint: a device that could name its own key could name
// another customer's room and file its alerts into it.
const deviceSiteSQL = `
	SELECT site_id FROM devices
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

// openRoom is the part of a room the fold needs: which one it is, and the span
// it already covers.
type openRoom struct {
	id        uuid.UUID
	firstSeen time.Time
	lastSeen  time.Time
}

// folding is one alert on its way into a room, with everything the journey
// needs: the transaction it shares with the alert's own insert, the machine's
// derived place in the tenancy ladder, and the two axes its rule groups on.
type folding struct {
	tx       *sql.Tx
	tenantID uuid.UUID
	alert    Alert
	grouping Grouping
	key      groupingKey
	now      time.Time
}

// fold files an alert into the room it belongs to, opening one when there is
// none. It runs inside the same transaction as the alert's own insert: an alert
// stored outside its room is invisible to the only surface a technician looks
// at, which is worse than one that never arrived, because nothing says it is
// missing.
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
		// Nothing can fold into this room any more, so it is closed at the
		// instant it became closeable and the alert starts a fresh one.
		if err := f.closeLapsed(ctx, room); err != nil {
			return err
		}
	case held:
		// The alert predates the room. It is not part of the story the room
		// tells, and the room is not stale, so the alert is kept and filed under
		// nothing rather than back-dated into work that is already under way.
		return nil
	}

	opens, err := f.opensARoom(ctx)
	if err != nil || !opens {
		return err
	}
	return f.openRoom(ctx)
}

// scopeKeyFor derives what a room is about from the machine's own place in the
// tenancy ladder.
//
// A machine filed into no office cannot be grouped with one, and pooling every
// unfiled machine under one absent key would put unrelated estates in a room
// with no correct assignee — so the room narrows to the machine itself, the
// narrowest thing that can honestly be named.
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

// lockOpenRoom reads and holds the open room this alert would join.
func (f folding) lockOpenRoom(ctx context.Context) (openRoom, bool, error) {
	return lockOpenRoomForKey(ctx, f.tx, f.key)
}

// lockOpenRoomForKey reads the open room holding a grouping key and keeps it for
// the rest of the transaction, so a concurrent fold cannot decide anything about
// the same key underneath the caller.
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

// closeLapsed ends a room nothing can still fold into, at the instant it became
// closeable rather than the moment the next alert happened to arrive.
func (f folding) closeLapsed(ctx context.Context, room openRoom) error {
	at := room.lastSeen.Add(f.grouping.Window)
	if _, err := f.tx.ExecContext(ctx, closeLapsedRoomSQL, room.id, at); err != nil {
		return fmt.Errorf("close lapsed incident: %w", err)
	}
	return nil
}

// opensARoom reports whether this alert is enough to raise one on its own.
//
// Anything a rule calls a warning or worse is. A low-severity observation is
// not: one host reading a little slower than usual is noise, and an estate-wide
// event that no single host breaches on is visible only because several hosts
// see it at once. So an observation waits, holding no room, until a second
// machine reports the same thing inside the window.
func (f folding) opensARoom(ctx context.Context) (bool, error) {
	if f.alert.Severity != SeverityInfo {
		return true, nil
	}
	if f.key.scope == ScopeDevice {
		// Cross-device co-occurrence cannot happen inside a room about one
		// machine, so an observation there never raises one.
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

// openRoom opens the room for a grouping key and gathers into it both this alert
// and any observations that were waiting for something to belong to.
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

// coOccurrenceWindow is how far either side of this alert an observation still
// counts as the same event. Two-sided for the same reason the fold is: a
// retroactive scan produces its findings in whatever order it walks history.
func (f folding) coOccurrenceWindow() (from, to time.Time) {
	return f.alert.ObservedAt.Add(-f.grouping.Window), f.alert.ObservedAt.Add(f.grouping.Window)
}

// fileInto puts the alert in a room and restates what the room says about
// itself.
func (f folding) fileInto(ctx context.Context, id uuid.UUID) error {
	if _, err := f.tx.ExecContext(ctx, attachAlertSQL, id, f.alert.ID); err != nil {
		return fmt.Errorf("file alert into incident: %w", err)
	}
	if _, err := f.tx.ExecContext(ctx, restateRoomFromItsAlertsSQL, id); err != nil {
		return fmt.Errorf("restate incident from its alerts: %w", err)
	}
	return nil
}

// groupingKey is what a room is about: the customer, the rule, and the rung
// of the tenancy ladder the room is filed under. It is what the fold resolves
// an alert to, and what decides whether a closed room may be reopened.
type groupingKey struct {
	organizationID uuid.UUID
	ruleID         string
	scope          Scope
	scopeKey       uuid.UUID
}
