package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// How long the investigation tables keep what nobody deleted.
//
// Erasure runs off a subject: purging a machine or a customer takes its alerts,
// evidence and rooms with it, immediately. This is the other axis — age — and it
// is what makes a declared retention period the actual one rather than a
// sentence in a document.

// retentionBatch bounds one delete. These tables only grow, so a first pass
// after the horizon is introduced can face a year of accumulation at once, and
// a single unbounded DELETE would hold locks and write WAL for as long as that
// takes. Each batch is its own transaction; the pass repeats until nothing is
// left to reclaim, so the bound costs passes rather than completeness.
const retentionBatch = 5000

// ErrHorizonNotPositive is returned for a retention horizon of zero or less. A
// zero horizon would make the cutoff `now`, which deletes everything the moment
// it is written — a misconfiguration this refuses rather than performs.
var ErrHorizonNotPositive = errors.New("retention horizon must be positive")

// deleteExpiredAlertsSQL removes one batch of alerts held longer than the
// horizon, evidence included — the blob is a column on the row, so it goes when
// the row does.
//
// received_at is the clock, not observed_at. A retroactive finding is
// legitimately months old when it arrives, and ageing it out on event time would
// delete it on the day it landed, before anyone had a chance to read it.
//
// Like the auto-resolve sweep, this statement names no tenant: it is asked about
// all of them, and a row in a tenant nobody happens to be serving requests for
// ages exactly the same way. It runs admin-scoped for the same reason a purge
// does.
const deleteExpiredAlertsSQL = `
	DELETE FROM alerts
	 WHERE ctid IN (
	     SELECT ctid FROM alerts
	      WHERE received_at < $1::timestamptz
	      LIMIT $2
	 )`

// deleteExpiredRoomsSQL removes one batch of rooms closed longer ago than the
// horizon, taking each room's history with it through the cascade on
// incident_events.
//
// A room is only removed once no alert points at it any more. An alert whose
// incident_id is cleared survives as a finding attached to no investigation,
// which reads as something nobody ever looked at — so the room outlives its
// alerts by construction, and the pass that removes the alerts is the one that
// makes the room eligible.
//
// An open room is never a candidate at any age: it is somebody's outstanding
// work, and the only thing that closes a room is the auto-resolve hold or a
// person.
const deleteExpiredRoomsSQL = `
	DELETE FROM incidents
	 WHERE ctid IN (
	     SELECT i.ctid FROM incidents i
	      WHERE i.status = 'resolved'
	        AND i.resolved_at < $1::timestamptz
	        AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.incident_id = i.id)
	      LIMIT $2
	 )`

// SweepExpired removes every alert held longer than horizon and every closed
// room left with nothing pointing at it, and returns how many rows it reclaimed.
//
// The two are swept in that order and not the other way round: a room's
// eligibility is decided by whether any alert still references it, so the alerts
// have to go first for the rooms to become removable in the same pass.
//
// It is idempotent — a second run over a swept store reclaims nothing — and it
// stops early on a cancelled context, returning what it had already reclaimed
// rather than discarding the count.
func (s *Store) SweepExpired(ctx context.Context, horizon time.Duration) (int, error) {
	if horizon <= 0 {
		return 0, fmt.Errorf("%w: got %s", ErrHorizonNotPositive, horizon)
	}
	cutoff := s.now().UTC().Add(-horizon)

	// Admin-scoped for the same reason the auto-resolve sweep is: the janitor
	// acts on every tenant, including the ones nobody is serving requests for.
	ctx = dbtx.WithDefaultTenant(ctx, true)

	total := 0
	for _, stage := range []struct {
		what string
		sql  string
	}{
		{"alerts", deleteExpiredAlertsSQL},
		{"incidents", deleteExpiredRoomsSQL},
	} {
		reclaimed, err := s.drain(ctx, stage.sql, cutoff)
		total += reclaimed
		if err != nil {
			return total, fmt.Errorf("sweep expired %s: %w", stage.what, err)
		}
	}
	return total, nil
}

// drain repeats one batched delete until a pass reclaims nothing. Each batch is
// its own transaction, so the locks a pass holds are bounded by the batch rather
// than by how far behind the sweep has fallen.
func (s *Store) drain(ctx context.Context, query string, cutoff time.Time) (int, error) {
	total := 0
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		var removed int64
		err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
			result, err := tx.ExecContext(ctx, query, cutoff, retentionBatch)
			if err != nil {
				return err
			}
			removed, err = result.RowsAffected()
			return err
		})
		if err != nil {
			return total, err
		}
		total += int(removed)
		if removed < retentionBatch {
			return total, nil
		}
	}
}
