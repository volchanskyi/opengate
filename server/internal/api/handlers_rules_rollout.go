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

// How far a rule spreads, how fast, and the switch that stops it.
//
// The stop is deliberately not the on/off toggle. Switching a rule off is an
// ordinary choice about what a customer wants watched; stopping it is an
// intervention against a rule that is doing harm, and afterwards the two have to
// be tellable apart — which they cannot be if one endpoint writes both.

// PutRuleRollout implements StrictServerInterface. There is nothing here for the
// automatic pull-back: it is the mitigation for a bad rule degrading an estate,
// so it is not configuration.
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

// pacedRollout applies the settings an operator stated to the state already
// stored. The stop and the reach are not this endpoint's to move: a stop is
// lifted deliberately, and how far a rule has reached belongs to the rollout
// machinery.
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

// applyStop reaches one customer or every customer in the tenant. The tenant
// scope is a statement over the tenant's own customers rather than a list this
// end assembles, so it cannot reach one customer short.
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

// stopAction names the write for the audit log. A stop and its lifting are
// different events, and reading them as one would make an audit trail that
// cannot answer whether a rule is running.
func stopAction(stopped bool) string {
	if stopped {
		return "rule.stop"
	}
	return "rule.resume"
}

// deliverRuleChange carries an administrator's change out to the machines that
// are already connected.
//
// A rule runs on the customer's own machines, on processor time they pay for,
// so a rule that turns out to be wrong has to be stoppable without waiting for
// anything. A healthy link is held open indefinitely — a machine re-registers
// only when something breaks it — so leaving the change for the next
// registration means leaving it for days on a stable estate, while the screen
// says it took effect.
//
// The push is best effort and never fails the change. What was asked for is
// already stored, a machine that could not be written to takes it as it
// reconnects, and refusing an administrator's stop because one link was in a
// bad state would be the worse answer.
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
