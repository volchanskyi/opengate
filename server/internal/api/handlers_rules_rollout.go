package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// PutRuleRollout implements StrictServerInterface.
func (s *Server) PutRuleRollout(ctx context.Context, request PutRuleRolloutRequestObject) (PutRuleRolloutResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PutRuleRollout403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	_, found, err := s.administrableRule(request.RuleId)
	if err != nil {
		return nil, err
	}
	if !found {
		return PutRuleRollout404JSONResponse{Error: msgRuleNotFound}, nil
	}

	organizationID, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	stored := s.pacedRollout(ctx, organizationID, request)

	switch err := s.ruleAdmin.UpsertRollout(ctx, stored); {
	case err == nil:
	case errors.Is(err, rules.ErrInvalidRollout):
		return PutRuleRollout400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.deliverRuleChange(ctx, organizationID, false)
	s.auditLog(ctx, ContextUserID(ctx), "rule.rollout.set", request.RuleId,
		fmt.Sprintf("enabled=%t canary=%d%% staged=%d%%",
			stored.Enabled, stored.CanaryPercent, stored.StagedPercent))
	return PutRuleRollout200JSONResponse(rolloutToAPI(stored)), nil
}

// pacedRollout applies the operator's settings to the stored state, leaving the stop and the reach.
func (s *Server) pacedRollout(
	ctx context.Context, organizationID uuid.UUID, request PutRuleRolloutRequestObject,
) rules.Rollout {
	stored := rules.RolloutFor(s.rolloutsFor(ctx, organizationID), organizationID, request.RuleId)
	stored.Enabled = request.Body.Enabled
	stored.CanaryPercent = request.Body.CanaryPercent
	stored.StagedPercent = request.Body.StagedPercent
	stored.CanaryHold = time.Duration(request.Body.CanaryHoldSecs) * time.Second
	stored.StagedHold = time.Duration(request.Body.StagedHoldSecs) * time.Second
	stored.UpdatedBy = ContextUserID(ctx).String()
	return stored
}

// StopRule implements StrictServerInterface.
func (s *Server) StopRule(ctx context.Context, request StopRuleRequestObject) (StopRuleResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, StopRule403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	_, found, err := s.administrableRule(request.RuleId)
	if err != nil {
		return nil, err
	}
	if !found {
		return StopRule404JSONResponse{Error: msgRuleNotFound}, nil
	}

	organizationID, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	if err := s.applyStop(ctx, request, organizationID, ContextUserID(ctx).String()); err != nil {
		return nil, err
	}

	s.deliverRuleChange(ctx, organizationID, request.Body.Scope == RuleStopScopeTenant)
	s.auditLog(ctx, ContextUserID(ctx), stopAction(request.Body.Stopped), request.RuleId,
		fmt.Sprintf("scope=%s", request.Body.Scope))
	return StopRule204Response{}, nil
}

// applyStop stops or resumes a rule for one customer or, tenant-wide, for every customer.
func (s *Server) applyStop(ctx context.Context, request StopRuleRequestObject, organizationID uuid.UUID, actor string) error {
	tenantWide := request.Body.Scope == RuleStopScopeTenant
	switch {
	case tenantWide && request.Body.Stopped:
		return s.ruleAdmin.StopRuleTenantWide(ctx, request.RuleId, actor)
	case tenantWide:
		return s.ruleAdmin.ResumeRuleTenantWide(ctx, request.RuleId, actor)
	case request.Body.Stopped:
		return s.ruleAdmin.StopRule(ctx, organizationID, request.RuleId, actor)
	default:
		return s.ruleAdmin.ResumeRule(ctx, organizationID, request.RuleId, actor)
	}
}

// stopAction names the audit-log event; a stop and its lifting are distinct events.
func stopAction(stopped bool) string {
	if stopped {
		return "rule.stop"
	}
	return "rule.resume"
}

// deliverRuleChange pushes a stored rule change to connected machines, best effort.
// A machine that cannot be reached takes the stored change when it reconnects.
func (s *Server) deliverRuleChange(ctx context.Context, organizationID uuid.UUID, tenantWide bool) {
	if s.agents == nil {
		return
	}
	if tenantWide {
		tenant, ok := dbtx.TenantFromContext(ctx)
		if !ok {
			return
		}
		s.logger.Info("delivered a tenant-wide rule change",
			"tenant_id", tenant.TenantID, "machines", s.agents.RefreshAlertRulesForTenant(ctx, tenant.TenantID))
		return
	}
	s.logger.Info("delivered a rule change",
		"organization_id", organizationID,
		"machines", s.agents.RefreshAlertRules(ctx, organizationID))
}
