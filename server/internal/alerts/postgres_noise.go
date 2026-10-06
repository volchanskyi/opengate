package alerts

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// ruleNoiseSQL counts one customer's recent alerts per rule beside the rule's own history.
// The organization predicate is required because row-level security stops only tenant crossings.
const ruleNoiseSQL = `
	SELECT rule_id,
	       COUNT(*) FILTER (WHERE received_at > $2::timestamptz) AS recent,
	       COUNT(*) FILTER (WHERE received_at <= $2::timestamptz) AS history
	  FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND organization_id = $1
	   AND received_at > $3::timestamptz
	 GROUP BY rule_id`

// RuleNoise reads how noisy each of one customer's rules has been lately, keyed by rule id.
// A rule absent from the result has raised nothing.
func (s *Store) RuleNoise(ctx context.Context, organizationID uuid.UUID) (map[string]Noise, error) {
	now := s.now().UTC()
	recentFrom := now.Add(-noiseWindow)
	historyFrom := now.Add(-noiseHistory)
	// The history spans the whole window except the recent hour the badge counts.
	historyHours := (noiseHistory - noiseWindow).Hours()

	out := make(map[string]Noise)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, ruleNoiseSQL, organizationID, recentFrom, historyFrom)
		if err != nil {
			return fmt.Errorf("read rule noise: %w", err)
		}
		defer rows.Close()

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
