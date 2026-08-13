package rules

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// A rule's rollout state, stored and read back.

const (
	upsertRolloutSQL = `INSERT INTO rule_rollout
		   (tenant_id, organization_id, rule_id, enabled, canary_group,
		    rollout_percent, kill, stage_entered_at, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), $8)
		 ON CONFLICT (organization_id, rule_id)
		 DO UPDATE SET enabled         = EXCLUDED.enabled,
		               canary_group    = EXCLUDED.canary_group,
		               rollout_percent = EXCLUDED.rollout_percent,
		               kill            = EXCLUDED.kill,
		               updated_at      = NOW(),
		               updated_by      = EXCLUDED.updated_by`

	listRolloutsSQL = `SELECT organization_id, rule_id, enabled, canary_group, rollout_percent,
		        kill, stage_entered_at, updated_at, updated_by
		   FROM rule_rollout
		  WHERE ` + scopedToTenant + ` AND organization_id = $1
		  ORDER BY rule_id`
)

// UpsertRollout stores one customer's rollout state for one rule.
func (s *Store) UpsertRollout(ctx context.Context, r Rollout) error {
	if err := ValidateRollout(r); err != nil {
		return err
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	return s.exec(ctx, "upsert rule rollout", upsertRolloutSQL,
		tenant, r.OrganizationID, r.RuleID, r.Enabled, r.CanaryGroup,
		r.RolloutPercent, r.Kill, r.UpdatedBy)
}

// ListRollouts returns one customer's stored rollout state, keyed by rule id. A
// rule absent from the result has no stored state, which is not the same as
// being switched off — see RolloutFor.
func (s *Store) ListRollouts(ctx context.Context, organizationID uuid.UUID) (map[string]Rollout, error) {
	out := make(map[string]Rollout)
	err := s.eachRow(ctx, "list rule rollouts", listRolloutsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var r Rollout
			if err := rows.Scan(&r.OrganizationID, &r.RuleID, &r.Enabled, &r.CanaryGroup,
				&r.RolloutPercent, &r.Kill, &r.StageEnteredAt, &r.UpdatedAt, &r.UpdatedBy); err != nil {
				return fmt.Errorf("scan rule rollout: %w", err)
			}
			out[r.RuleID] = r
			return nil
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// RolloutFor returns the rollout that applies to a rule, falling back to the
// shipped default when the customer has no stored row. Reading a missing row as
// the zero value would leave a fresh customer silently unmonitored, so the
// fallback is stated here and used by every caller.
func RolloutFor(stored map[string]Rollout, organizationID uuid.UUID, ruleID string) Rollout {
	if r, ok := stored[ruleID]; ok {
		return r
	}
	return DefaultRollout(organizationID, ruleID)
}
