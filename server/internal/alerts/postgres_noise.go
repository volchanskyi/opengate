package alerts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The read behind the badge on every rule in the pack.

// ruleNoiseSQL counts one customer's recent alerts per rule beside the rule's
// own history, in one grouped pass — the badge is drawn for every rule on one
// screen, so a query per rule would be a read per row of a list.
//
// The customer is named explicitly. Row-level security stops this crossing a
// tenant, and nothing at all stops it crossing a customer inside one: without
// the predicate the badge would show a number belonging to somebody else, and it
// would look entirely plausible.
const ruleNoiseSQL = `
	SELECT rule_id,
	       COUNT(*) FILTER (WHERE received_at > $2::timestamptz) AS recent,
	       COUNT(*) FILTER (WHERE received_at <= $2::timestamptz) AS history
	  FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1
	   AND received_at > $3::timestamptz
	 GROUP BY rule_id`

// RuleNoise reads how noisy each of one customer's rules has been lately, keyed
// by rule id. A rule absent from the result has raised nothing at all.
func (s *Store) RuleNoise(ctx context.Context, organizationID uuid.UUID) (map[string]Noise, error) {
	now := s.now().UTC()
	recentFrom := now.Add(-noiseWindow)
	historyFrom := now.Add(-noiseHistory)
	// The hours the history is averaged over: everything in the window that is
	// not the recent hour the badge is about.
	historyHours := (noiseHistory - noiseWindow).Hours()

	out := make(map[string]Noise)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, ruleNoiseSQL, organizationID, recentFrom, historyFrom)
		if err != nil {
			return fmt.Errorf("read rule noise: %w", err)
		}
		defer rows.Close() //nolint:errcheck // read-only; rows.Err below is the check

		for rows.Next() {
			var (
				ruleID          string
				recent, history int
			)
			if err := rows.Scan(&ruleID, &recent, &history); err != nil {
				return fmt.Errorf("scan rule noise: %w", err)
			}
			out[ruleID] = Noise{
				RuleID:          ruleID,
				Recent:          recent,
				BaselinePerHour: float64(history) / historyHours,
				HasHistory:      history > 0,
			}
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
