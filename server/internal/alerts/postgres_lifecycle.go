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

// readRoomStatusSQL reads where a room stands and locks it for the transaction.
// The lock keeps two people from moving it from the state they each read.
const readRoomStatusSQL = `
	SELECT status, organization_id, rule_id, scope, scope_key
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1
	   FOR UPDATE`

// applyTransitionSQL moves a room: a resolution stamps a time and an answer, any other move
// clears both so reopening withdraws the answer.
const applyTransitionSQL = `
	UPDATE incidents
	   SET status      = $2::text,
	       resolved_at = CASE WHEN $2::text = 'resolved' THEN $3::timestamptz END,
	       cause_code  = NULLIF($4::text, '')
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

// appendRoomEventSQL adds one line to a room's history, taking tenant and customer from the room.
// The caller supplies the line's id so a comment can be named back to its author.
const appendRoomEventSQL = `
	INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, actor_id, body)
	SELECT $6::uuid, i.tenant_id, i.organization_id, i.id, $2::timestamptz, $3::text,
	       NULLIF($4::text, '')::uuid, $5::jsonb
	  FROM incidents i
	 WHERE i.tenant_id = current_setting('app.current_tenant')::uuid AND i.id = $1`

// resolveStaleRoomsSQL closes every room idle past its rule's hold, naming no tenant on purpose.
// Only a device room for a machine in maintenance is shielded from closing.
const resolveStaleRoomsSQL = `
	WITH hold(rule_id, secs) AS (
	    SELECT * FROM unnest($1::text[], $2::double precision[])
	), lapsed AS (
	    UPDATE incidents i
	       SET status = 'resolved',
	           resolved_at = i.last_seen + make_interval(secs => h.secs)
	      FROM hold h
	     WHERE i.rule_id = h.rule_id
	       AND i.status <> 'resolved'
	       AND i.last_seen + make_interval(secs => h.secs) <= $3::timestamptz
	       AND NOT (i.scope = 'device'
	                AND EXISTS (SELECT 1 FROM devices d
	                             WHERE d.id = i.scope_key AND d.maintenance_on))
	    RETURNING i.id, i.tenant_id, i.organization_id, i.resolved_at
	)
	INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, body)
	SELECT gen_random_uuid(), tenant_id, organization_id, id, resolved_at, 'resolution',
	       '{"reason": "no alert within the reopen window"}'::jsonb
	  FROM lapsed`

// Transition moves an incident to a new status and records who moved it in the room's history.
// A resolution must carry a cause code.
func (s *Store) Transition(ctx context.Context, incidentID uuid.UUID, change Change) error {
	at := s.now().UTC().Truncate(time.Microsecond)
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		current, _, err := roomUnderChange(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if err := change.check(current); err != nil {
			return err
		}
		return applyChange(ctx, tx, incidentID, at, change, transitionBody{
			From: current, To: change.To, Cause: change.Cause,
		})
	})
}

// Reopen takes a closed incident back into investigation and withdraws its cause code.
// It fails with ErrKeyAlreadyOpen when a fresh room already holds the grouping key.
func (s *Store) Reopen(ctx context.Context, incidentID uuid.UUID, actor uuid.UUID) error {
	at := s.now().UTC().Truncate(time.Microsecond)
	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		current, key, err := roomUnderChange(ctx, tx, incidentID)
		if err != nil {
			return err
		}
		if current != StatusResolved {
			return fmt.Errorf("%w: %s is already open", ErrIllegalTransition, current)
		}
		successor, taken, err := lockOpenRoomForKey(ctx, tx, key)
		if err != nil {
			return err
		}
		if taken {
			return fmt.Errorf("%w: %s", ErrKeyAlreadyOpen, successor.id)
		}

		change := Change{To: StatusInvestigating, Actor: actor}
		return applyChange(ctx, tx, incidentID, at, change, transitionBody{
			From: current, To: change.To, Reopened: true,
		})
	})
}

// roomUnderChange reads and locks a room for the transaction.
// A room in another tenant answers the same as one that does not exist.
func roomUnderChange(ctx context.Context, tx *sql.Tx, incidentID uuid.UUID) (Status, groupingKey, error) {
	var (
		status string
		key    groupingKey
		scope  string
	)
	switch err := tx.QueryRowContext(ctx, readRoomStatusSQL, incidentID).
		Scan(&status, &key.organizationID, &key.ruleID, &scope, &key.scopeKey); {
	case errors.Is(err, sql.ErrNoRows):
		return "", groupingKey{}, fmt.Errorf("%w: %s", ErrIncidentNotFound, incidentID)
	case err != nil:
		return "", groupingKey{}, fmt.Errorf("read incident for transition: %w", err)
	}
	key.scope = Scope(scope)
	return Status(status), key, nil
}

// applyChange writes the move and the line of history that explains it.
func applyChange(
	ctx context.Context, tx *sql.Tx, incidentID uuid.UUID,
	at time.Time, change Change, body transitionBody,
) error {
	if _, err := tx.ExecContext(ctx, applyTransitionSQL,
		incidentID, string(change.To), at, string(change.Cause)); err != nil {
		return fmt.Errorf("move incident: %w", err)
	}
	encoded, err := body.json()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, appendRoomEventSQL,
		incidentID, at, eventKind(change.To), actorArg(change.Actor), encoded, uuid.New()); err != nil {
		return fmt.Errorf("record incident transition: %w", err)
	}
	return nil
}

// actorArg renders who did it, empty when the system did.
func actorArg(actor uuid.UUID) string {
	if actor == uuid.Nil {
		return ""
	}
	return actor.String()
}

// ResolveStale closes every room idle past its rule's hold and returns the count.
// windows maps a rule id to its hold; a room whose rule has no window stays open.
func (s *Store) ResolveStale(ctx context.Context, windows map[string]time.Duration) (int, error) {
	rules, seconds := holds(windows)
	at := s.now().UTC().Truncate(time.Microsecond)

	// The janitor acts on every tenant, so it runs admin-scoped.
	ctx = dbtx.WithDefaultTenant(ctx, true)
	var closed int64
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, resolveStaleRoomsSQL, rules, seconds, at)
		if err != nil {
			return fmt.Errorf("resolve stale incidents: %w", err)
		}
		closed, err = result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count resolved incidents: %w", err)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(closed), nil
}

// holds turns the caller's windows into the two arrays the sweep joins on and adds the storm hold.
func holds(windows map[string]time.Duration) ([]string, []float64) {
	rules := make([]string, 0, len(windows)+1)
	seconds := make([]float64, 0, len(windows)+1)
	for ruleID, window := range windows {
		if ruleID == StormRuleID || window <= 0 {
			continue
		}
		rules = append(rules, ruleID)
		seconds = append(seconds, window.Seconds())
	}
	return append(rules, StormRuleID), append(seconds, StormHold.Seconds())
}
