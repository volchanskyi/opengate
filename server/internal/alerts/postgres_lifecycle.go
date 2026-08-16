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

// Where a room stands, how it is allowed to move, and when the system ends one
// on its own.
//
// A room in `new` *is* the triage queue, so these are the queue's rules. Its own
// columns say where it stands; the event rows say how it got there, which is
// what a handover between two technicians reads.

// readRoomStatusSQL reads where a room stands and what it is about, and holds it
// for the rest of the transaction so two people cannot both move it from the
// state they each read.
const readRoomStatusSQL = `
	SELECT status, organization_id, rule_id, scope, scope_key
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1
	   FOR UPDATE`

// applyTransitionSQL moves a room. Resolution stamps a time and an answer;
// anything else clears both, which is what makes reopening withdraw the answer
// rather than leave a closed room's reason attached to an open one.
const applyTransitionSQL = `
	UPDATE incidents
	   SET status      = $2::text,
	       resolved_at = CASE WHEN $2::text = 'resolved' THEN $3::timestamptz END,
	       cause_code  = NULLIF($4::text, '')
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

// appendRoomEventSQL adds one line to a room's history. The tenant and the
// customer are read from the room itself rather than passed in, so an event can
// never be filed against a customer its own room does not belong to.
//
// The line's own id comes from the caller, because a comment is handed back to
// the person who wrote it and needs a name they can refer to it by.
const appendRoomEventSQL = `
	INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, actor_id, body)
	SELECT $6::uuid, i.tenant_id, i.organization_id, i.id, $2::timestamptz, $3::text,
	       NULLIF($4::text, '')::uuid, $5::jsonb
	  FROM incidents i
	 WHERE i.tenant_id = current_setting('app.current_tenant')::uuid AND i.id = $1`

// resolveStaleRoomsSQL closes every room whose last alert is older than its
// rule's hold, across every tenant at once.
//
// This is the one statement in the store that names no tenant, and deliberately:
// it is asked about all of them, so there is nothing for a predicate to confine
// it to, and a stale room in a tenant nobody happens to be serving requests for
// still sits in that tenant's triage queue. It runs admin-scoped for the same
// reason a purge does.
//
// A machine in maintenance keeps its room, and the check is part of this
// statement rather than a decision taken beforehand: maintenance stops the agent
// sampling, so the silence that follows is the silence the operator asked for,
// and reading it as recovery closes the very incident the host work is happening
// because of. The shield is only for a room about that one machine — a customer
// or site room is still being reported into by the rest of the estate, and
// shielding those would let one machine parked in maintenance pin an estate's
// rooms open indefinitely.
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

// Transition moves an incident to a new status and records who moved it.
//
// The room's own columns say where it stands; the event row says how it got
// there, which is what a handover between two technicians reads. A resolution
// must carry an answer for why — `false_positive` is the only channel that says
// which curated rule needs its threshold moved, so a resolution that skips it
// spends feedback the rule pack is tuned from.
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

// Reopen takes a closed incident back into investigation and withdraws the
// answer that was given for closing it.
//
// It is a door of its own rather than an ordinary transition because it undoes
// something already recorded: a technician who closed an incident that was not
// fixed has to be able to say so, and that is a different act from carrying on
// with an open room. It fails when the same condition has already recurred and
// opened a fresh room — there is exactly one open room per grouping key, and the
// live one is where the alerts are landing.
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

// roomUnderChange reads and holds a room for the rest of the transaction, so two
// people cannot both move it from the state they each read. A room in another
// tenant answers the same as one that does not exist.
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

// ResolveStale closes every room whose last alert is older than its rule's hold
// and returns how many it closed. windows maps a rule id to its grouping window,
// which is the hold: a room stays open for exactly as long as a new alert could
// still fold into it.
//
// A room raised by a rule this build no longer ships is left alone. There is
// nothing to measure its hold against, and closing a customer's open work on a
// guessed number is worse than leaving it for a person.
func (s *Store) ResolveStale(ctx context.Context, windows map[string]time.Duration) (int, error) {
	rules, seconds := holds(windows)
	at := s.now().UTC().Truncate(time.Microsecond)

	// Admin-scoped for the same reason a purge is: the janitor acts on every
	// tenant, including the ones nobody is currently serving requests for.
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

// holds turns the caller's windows into the two arrays the sweep joins on, and
// adds the storm room's own hold — no catalogue rule can supply that one,
// because the storm room is not a rule.
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
