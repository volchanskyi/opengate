package alerts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The platform's own view of the investigation tables: how much unresolved work
// they are holding, across the whole install.
//
// It is deliberately about open work rather than about rows. Both tables only
// grow — retention is a later program — so a count of everything in them would
// climb forever and say nothing about whether anybody is falling behind. What a
// gauge can usefully say is how much is still being worked, and how much detail
// is attached to it.
//
// It is also deliberately about every tenant at once. The exported series carry
// no tenant label — this is the platform's monitoring of itself, not a
// customer's view — so a triage queue in a tenant this process is not currently
// serving requests for still has to be in the number. That is the same reason
// the stale-room janitor runs admin-scoped.

// openInvestigationsSQL counts the rooms that are still open, split by where each
// one stands, and the alerts sitting in them.
//
// One aggregate rather than a query per status and a second pass for the alerts:
// the caller refreshes a gauge from this on a timer, and a read that costs one
// round trip per status is a read somebody will eventually move into the scrape
// path. The join is driven from open incidents and reaches the alerts through
// their incident index, so its cost tracks the size of the queue rather than the
// size of the table.
//
// Like the stale-room sweep, it names no tenant — it is asked about all of them,
// so there is nothing for a predicate to confine it to.
const openInvestigationsSQL = `
	SELECT i.status, COUNT(DISTINCT i.id), COUNT(a.id)
	  FROM incidents i
	  LEFT JOIN alerts a ON a.incident_id = i.id
	 WHERE i.status <> 'resolved'
	 GROUP BY i.status`

// OpenStatuses is every status an incident that is not over can hold. A resolved
// incident is not open work, which is the whole distinction: the set here is the
// lifecycle minus its end.
func OpenStatuses() []Status {
	return []Status{StatusNew, StatusAcknowledged, StatusInvestigating}
}

// OpenInvestigations returns how many incidents are open in each status, and how
// many alerts are sitting in them, across every tenant.
//
// A status nothing is open in is absent from the result rather than present as
// zero — the caller exports the closed vocabulary and reads a missing status as
// none, which keeps this read to the rows the database actually has.
//
// It scopes itself, because its caller is a background refresh belonging to no
// request and has no tenant to pass.
func (s *Store) OpenInvestigations(ctx context.Context) (map[string]int, int, error) {
	ctx = dbtx.WithDefaultTenant(ctx, true)

	byStatus := make(map[string]int)
	var openAlerts int
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, openInvestigationsSQL)
		if err != nil {
			return fmt.Errorf("count open investigations: %w", err)
		}
		// Read-only, so the close itself has nothing to report; rows.Err below is
		// the check that matters.
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			var (
				status              string
				incidents, inFlight int
			)
			if err := rows.Scan(&status, &incidents, &inFlight); err != nil {
				return fmt.Errorf("scan open investigations: %w", err)
			}
			byStatus[status] = incidents
			openAlerts += inFlight
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read open investigations: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	return byStatus, openAlerts, nil
}
