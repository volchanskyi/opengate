package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// The numbers a customer may retune, and the moves a rule upgrade made to them.
//
// A value outside what the rule allows is refused here, where an operator can
// still see why, rather than reaching five thousand endpoints and being
// discovered later from the alerts it did not raise.

// PutRuleBinding implements StrictServerInterface.
func (s *Server) PutRuleBinding(ctx context.Context, request PutRuleBindingRequestObject) (PutRuleBindingResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, PutRuleBinding403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	_, found, err := s.administrableRule(request.RuleId)
	if err != nil {
		return nil, err
	}
	if !found {
		return PutRuleBinding404JSONResponse{Error: msgRuleNotFound}, nil
	}

	organizationID, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	binding := bindingFromAPI(request.RuleId, organizationID, *request.Body)
	binding.UpdatedBy = ContextUserID(ctx).String()

	switch err := s.ruleAdmin.UpsertBinding(ctx, s.ruleCatalogue, binding); {
	case err == nil:
	case errors.Is(err, rules.ErrParamOutOfBounds),
		errors.Is(err, rules.ErrParamNotTunable),
		errors.Is(err, rules.ErrInvalidSelector),
		errors.Is(err, rules.ErrInvalidLevel),
		errors.Is(err, rules.ErrUnknownRule):
		return PutRuleBinding400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "rule.binding.set", request.RuleId,
		fmt.Sprintf("level=%s params=%v", request.Body.Level, request.Body.Params))
	return PutRuleBinding200JSONResponse(bindingToAPI(binding)), nil
}

// DeleteRuleBinding implements StrictServerInterface. The machines the value
// covered fall back to the next rung up, which is the point of removing it.
func (s *Server) DeleteRuleBinding(ctx context.Context, request DeleteRuleBindingRequestObject) (DeleteRuleBindingResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteRuleBinding403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errRulesNotAdministrable
	}
	if err := s.ruleAdmin.DeleteBinding(ctx, request.BindingId); err != nil {
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "rule.binding.delete", request.RuleId,
		request.BindingId.String())
	return DeleteRuleBinding204Response{}, nil
}

// AcknowledgeRuleClamp implements StrictServerInterface. A move a rule version
// made stays on the screen until somebody has seen it, because it was not the
// customer's decision.
func (s *Server) AcknowledgeRuleClamp(ctx context.Context, request AcknowledgeRuleClampRequestObject) (AcknowledgeRuleClampResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, AcknowledgeRuleClamp403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errRulesNotAdministrable
	}

	switch err := s.ruleAdmin.AcknowledgeClamp(ctx, request.ClampId, ContextUserID(ctx).String()); {
	case err == nil:
	case errors.Is(err, rules.ErrClampNotFound):
		return AcknowledgeRuleClamp404JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "rule.clamp.acknowledge", request.RuleId,
		request.ClampId.String())
	return AcknowledgeRuleClamp204Response{}, nil
}
