package rules

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// A rule's rollout state, stored and read back.

const (
	upsertRolloutSQL = `INSERT INTO rule_rollout
		   (tenant_id, organization_id, rule_id, enabled, canary_group,
		    rollout_percent, kill, canary_percent, staged_percent,
		    canary_hold_secs, staged_hold_secs, stage_entered_at, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW(), $12)
		 ON CONFLICT (organization_id, rule_id)
		 DO UPDATE SET enabled          = EXCLUDED.enabled,
		               canary_group     = EXCLUDED.canary_group,
		               rollout_percent  = EXCLUDED.rollout_percent,
		               kill             = EXCLUDED.kill,
		               canary_percent   = EXCLUDED.canary_percent,
		               staged_percent   = EXCLUDED.staged_percent,
		               canary_hold_secs = EXCLUDED.canary_hold_secs,
		               staged_hold_secs = EXCLUDED.staged_hold_secs,
		               updated_at       = NOW(),
		               updated_by       = EXCLUDED.updated_by`

	// A stop is filed on the customer, and it is the one write that leaves
	// everything else alone: whether the customer wanted the rule, and how far it
	// had reached, are still true and are what a resume goes back to.
	stopRuleSQL = `INSERT INTO rule_rollout
		   (tenant_id, organization_id, rule_id, kill, stage_entered_at, updated_at, updated_by)
		 VALUES ($1, $2, $3, $4, NOW(), NOW(), $5)
		 ON CONFLICT (organization_id, rule_id)
		 DO UPDATE SET kill       = EXCLUDED.kill,
		               updated_at = NOW(),
		               updated_by = EXCLUDED.updated_by`

	// The same stop, for every customer in the tenant at once. It is a row per
	// customer rather than a tenant-level flag, because that is what the delivery
	// path already reads — a flag somewhere else would be a second thing to
	// remember at exactly the moment nobody is remembering anything.
	stopRuleTenantWideSQL = `INSERT INTO rule_rollout
		   (tenant_id, organization_id, rule_id, kill, stage_entered_at, updated_at, updated_by)
		 SELECT o.tenant_id, o.id, $1, $2, NOW(), NOW(), $3
		   FROM organizations o
		  WHERE o.` + scopedToTenant + `
		 ON CONFLICT (organization_id, rule_id)
		 DO UPDATE SET kill       = EXCLUDED.kill,
		               updated_at = NOW(),
		               updated_by = EXCLUDED.updated_by`

	setRolloutStageSQL = `INSERT INTO rule_rollout
			   (tenant_id, organization_id, rule_id, enabled, canary_group,
			    rollout_percent, kill, stage_entered_at, updated_at, updated_by)
			 VALUES ($1, $2, $3, TRUE, '', $4, FALSE, NOW(), NOW(), $5)
			 ON CONFLICT (organization_id, rule_id)
			 DO UPDATE SET rollout_percent  = EXCLUDED.rollout_percent,
			               stage_entered_at = NOW(),
			               updated_at       = NOW(),
			               updated_by       = EXCLUDED.updated_by`

	listRolloutsSQL = `SELECT organization_id, rule_id, enabled, canary_group, rollout_percent,
		        kill, canary_percent, staged_percent, canary_hold_secs, staged_hold_secs,
		        stage_entered_at, updated_at, updated_by
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
	paced := r.paced()
	return s.exec(ctx, "upsert rule rollout", upsertRolloutSQL,
		tenant, r.OrganizationID, r.RuleID, r.Enabled, r.CanaryGroup,
		r.RolloutPercent, r.Kill, paced.CanaryPercent, paced.StagedPercent,
		int(paced.CanaryHold.Seconds()), int(paced.StagedHold.Seconds()), r.UpdatedBy)
}

// StopRule stops one rule for one customer. It is deliberately not the on/off
// toggle: switching a rule off is an ordinary choice and a stop is an
// intervention, and afterwards the two have to be tellable apart.
func (s *Store) StopRule(ctx context.Context, organizationID uuid.UUID, ruleID, updatedBy string) error {
	return s.setKill(ctx, organizationID, ruleID, true, updatedBy)
}

// ResumeRule lifts a stop for one customer, and lifts nothing else: a rule the
// customer had switched off is still off afterwards.
func (s *Store) ResumeRule(ctx context.Context, organizationID uuid.UUID, ruleID, updatedBy string) error {
	return s.setKill(ctx, organizationID, ruleID, false, updatedBy)
}

// StopRuleTenantWide stops one rule for every customer in the tenant. It is the
// action for a rule that is degrading estates rather than one customer's, and it
// reaches nobody outside the tenant — the statement's own scope is what decides
// that rather than a list the caller assembles.
func (s *Store) StopRuleTenantWide(ctx context.Context, ruleID, updatedBy string) error {
	return s.setKillTenantWide(ctx, ruleID, true, updatedBy)
}

// ResumeRuleTenantWide lifts a tenant-wide stop.
func (s *Store) ResumeRuleTenantWide(ctx context.Context, ruleID, updatedBy string) error {
	return s.setKillTenantWide(ctx, ruleID, false, updatedBy)
}

// setKill writes the stop for one customer.
func (s *Store) setKill(ctx context.Context, organizationID uuid.UUID, ruleID string, kill bool, updatedBy string) error {
	if ruleID == "" {
		return fmt.Errorf("%w: rule id is required", ErrInvalidRollout)
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	return s.exec(ctx, "stop rule", stopRuleSQL, tenant, organizationID, ruleID, kill, updatedBy)
}

// setKillTenantWide writes the stop for every customer in the tenant.
func (s *Store) setKillTenantWide(ctx context.Context, ruleID string, kill bool, updatedBy string) error {
	if ruleID == "" {
		return fmt.Errorf("%w: rule id is required", ErrInvalidRollout)
	}
	if _, err := callerTenant(ctx); err != nil {
		return err
	}
	return s.exec(ctx, "stop rule tenant-wide", stopRuleTenantWideSQL, ruleID, kill, updatedBy)
}

// SetRolloutStage moves a rule to another stage for one customer and stamps the
// moment it entered that stage, which is what the stage's hold is measured from.
// Leaving the original stamp would let a rule that has just been reverted count
// the hours it spent on the stage it failed and advance straight back into it.
//
// It writes the reach and nothing else. A kill, a rule the customer switched
// off, and the group they named are theirs; the rollout machinery moves a rule
// between stages and never undoes a stop somebody reached for.
func (s *Store) SetRolloutStage(ctx context.Context, organizationID uuid.UUID, ruleID string, percent int, updatedBy string) error {
	if err := ValidateRollout(Rollout{
		OrganizationID: organizationID,
		RuleID:         ruleID,
		RolloutPercent: percent,
	}); err != nil {
		return err
	}
	tenant, err := callerTenant(ctx)
	if err != nil {
		return err
	}
	return s.exec(ctx, "set rule rollout stage", setRolloutStageSQL,
		tenant, organizationID, ruleID, percent, updatedBy)
}

// ListRollouts returns one customer's stored rollout state, keyed by rule id. A
// rule absent from the result has no stored state, which is not the same as
// being switched off — see RolloutFor.
func (s *Store) ListRollouts(ctx context.Context, organizationID uuid.UUID) (map[string]Rollout, error) {
	out := make(map[string]Rollout)
	err := s.eachRow(ctx, "list rule rollouts", listRolloutsSQL, []any{organizationID},
		func(rows *sql.Rows) error {
			var (
				r                              Rollout
				canaryHoldSecs, stagedHoldSecs int
			)
			if err := rows.Scan(&r.OrganizationID, &r.RuleID, &r.Enabled, &r.CanaryGroup,
				&r.RolloutPercent, &r.Kill, &r.CanaryPercent, &r.StagedPercent,
				&canaryHoldSecs, &stagedHoldSecs,
				&r.StageEnteredAt, &r.UpdatedAt, &r.UpdatedBy); err != nil {
				return fmt.Errorf("scan rule rollout: %w", err)
			}
			r.CanaryHold = time.Duration(canaryHoldSecs) * time.Second
			r.StagedHold = time.Duration(stagedHoldSecs) * time.Second
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
