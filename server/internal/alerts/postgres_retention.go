package alerts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// retentionBatch bounds one delete so a first pass over a large backlog holds no long lock.
const retentionBatch = 5000

// ErrHorizonNotPositive reports a retention horizon of zero or less, which would delete everything.
var ErrHorizonNotPositive = errors.New("retention horizon must be positive")

// Age is received_at so a backfilled finding survives its arrival; the janitor spans all tenants.
const deleteExpiredAlertsSQL = `
	DELETE FROM alerts
	 WHERE ctid IN (
	     SELECT ctid FROM alerts
	      WHERE received_at < $1::timestamptz
	      LIMIT $2
	 )`

// A room is a candidate only once no alert points at it; its history goes by cascade.
const deleteExpiredRoomsSQL = `
	DELETE FROM incidents
	 WHERE ctid IN (
	     SELECT i.ctid FROM incidents i
	      WHERE i.status = 'resolved'
	        AND i.resolved_at < $1::timestamptz
	        AND NOT EXISTS (SELECT 1 FROM alerts a WHERE a.incident_id = i.id)
	      LIMIT $2
	 )`

// SweepExpired removes alerts past the horizon, then resolved rooms nothing points at, and returns
// the rows removed; a cancelled context returns the count reclaimed so far.
func (s *Store) SweepExpired(ctx context.Context, horizon time.Duration) (int, error) {
	if horizon <= 0 {
		return 0, fmt.Errorf("%w: got %s", ErrHorizonNotPositive, horizon)
	}
	cutoff := s.now().UTC().Add(-horizon)

	// The janitor acts on every tenant, so it runs admin-scoped.
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
