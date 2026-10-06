package alerts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// openInvestigationsSQL counts open incidents per status and the alerts in them.
// It is one aggregate so a timer-driven gauge refresh costs a single round trip.
const openInvestigationsSQL = `
	SELECT i.status, COUNT(DISTINCT i.id), COUNT(a.id)
	  FROM incidents i
	  LEFT JOIN alerts a ON a.incident_id = i.id
	 WHERE i.status <> 'resolved'
	 GROUP BY i.status`

// OpenStatuses is every status an incident that is not over can hold: the lifecycle minus resolved.
func OpenStatuses() []Status {
	return []Status{StatusNew, StatusAcknowledged, StatusInvestigating}
}

// OpenInvestigations returns open incidents per status and the alerts in them, across every tenant.
// It scopes itself admin-wide because its caller is a background refresh with no tenant.
func (s *Store) OpenInvestigations(ctx context.Context) (map[string]int, int, error) {
	ctx = dbtx.WithDefaultTenant(ctx, true)

	byStatus := make(map[string]int)
	var openAlerts int
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, openInvestigationsSQL)
		if err != nil {
			return fmt.Errorf("count open investigations: %w", err)
		}
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
