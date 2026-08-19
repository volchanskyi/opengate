package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// One rule as an operator reads it, and as one named machine is running it.
//
// Both are open to every member of the tenant. A technician resolving something
// as a false alarm has to be able to see the rule that produced it, and the
// resolved read is what answers the question the tuning exists for — why is this
// machine at 95? — by walking the same resolution the delivery path walks.

// GetRule implements StrictServerInterface.
func (s *Server) GetRule(ctx context.Context, request GetRuleRequestObject) (GetRuleResponseObject, error) {
	if s.ruleCatalogue == nil {
		return nil, errRulesUnavailable
	}
	definition, ok := s.ruleCatalogue.Lookup(request.RuleId)
	if !ok {
		return GetRule404JSONResponse{Error: msgRuleNotFound}, nil
	}
	organizationID := deref(request.Params.OrganizationId)

	counts, err := s.devices.Counts(ctx, device.OrganizationID(organizationID))
	if err != nil {
		return nil, err
	}

	detail := RuleDetail{
		Rule: ruleToAPI(definition,
			s.rolloutsFor(ctx, organizationID)[definition.ID],
			s.coverageFor(ctx, organizationID, counts.Total)[definition.ID],
			counts.Total,
			s.noiseFor(ctx, organizationID)[definition.ID]),
		Bindings: bindingsToAPI(s.bindingsFor(ctx, organizationID, definition.ID)),
		Clamps:   clampsToAPI(s.clampsFor(ctx, organizationID, definition.ID)),
	}
	return GetRule200JSONResponse(detail), nil
}

// GetResolvedRule implements StrictServerInterface.
func (s *Server) GetResolvedRule(ctx context.Context, request GetResolvedRuleRequestObject) (GetResolvedRuleResponseObject, error) {
	if s.ruleCatalogue == nil {
		return nil, errRulesUnavailable
	}
	definition, ok := s.ruleCatalogue.Lookup(request.RuleId)
	if !ok {
		return GetResolvedRule404JSONResponse{Error: msgRuleNotFound}, nil
	}

	// The machine is looked up inside the caller's tenant, which is what makes
	// naming another tenant's machine resolve to nothing rather than to an
	// answer about somebody else's estate.
	target, err := s.devices.Get(ctx, request.Params.DeviceId)
	if err != nil {
		if errors.Is(err, device.ErrDeviceNotFound) {
			return GetResolvedRule404JSONResponse{Error: msgDeviceNotFound}, nil
		}
		return nil, err
	}

	counts, err := s.devices.Counts(ctx, device.OrganizationID(target.OrganizationID))
	if err != nil {
		return nil, err
	}

	machine := rules.Device{
		Scope: settings.Scope{
			DeviceID:       target.ID,
			SiteID:         target.SiteID,
			OrganizationID: target.OrganizationID,
		},
		Tags:      s.tagsFor(ctx, target.ID),
		FleetSize: counts.Total,
	}
	bindings := s.bindingsFor(ctx, target.OrganizationID, definition.ID)
	rollout := rules.RolloutFor(s.rolloutsFor(ctx, target.OrganizationID),
		target.OrganizationID, definition.ID)

	return GetResolvedRule200JSONResponse(ResolvedRule{
		RuleId:    definition.ID,
		DeviceId:  target.ID,
		Delivered: rollout.Reaches(target.ID, machine.FleetSize),
		Params:    resolvedParamsToAPI(definition, machine, bindings),
	}), nil
}

// bindingsFor reads one customer's tuning for one rule.
//
// A read that fails leaves the rule reading as untuned rather than failing the
// page, which is the same trade the rollout read makes: the description of the
// rule is what somebody opened this for and it is still true.
func (s *Server) bindingsFor(ctx context.Context, organizationID uuid.UUID, ruleID string) []rules.Binding {
	if s.ruleAdmin == nil {
		return nil
	}
	stored, err := s.ruleAdmin.ListBindings(ctx, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read rule tuning failed",
			"organization_id", organizationID, "error", err)
		return nil
	}
	return forRule(stored, ruleID, func(b rules.Binding) string { return b.RuleID })
}

// clampsFor reads what a rule version had to move, for one rule. It reconciles
// against the pack as it now stands first — a rule upgrade that narrowed a range
// has to be visible on the screen that shows the tuning, not only on the wire.
func (s *Server) clampsFor(ctx context.Context, organizationID uuid.UUID, ruleID string) []rules.Clamp {
	if s.ruleAdmin == nil || s.ruleCatalogue == nil {
		return nil
	}
	outstanding, err := s.ruleAdmin.ReconcileClamps(ctx, s.ruleCatalogue, organizationID)
	if err != nil {
		s.logger.WarnContext(ctx, "read rule clamps failed",
			"organization_id", organizationID, "error", err)
		return nil
	}
	return forRule(outstanding, ruleID, func(c rules.Clamp) string { return c.RuleID })
}

// tagsFor reads the labels one machine carries, which narrow the tuning that
// applies to it.
func (s *Server) tagsFor(ctx context.Context, deviceID uuid.UUID) map[string]string {
	if s.ruleAdmin == nil {
		return nil
	}
	tags, err := s.ruleAdmin.TagsFor(ctx, deviceID)
	if err != nil {
		s.logger.WarnContext(ctx, "read device labels failed", "device_id", deviceID, "error", err)
		return nil
	}
	return tags
}

// forRule keeps the entries belonging to one rule. Both stores answer for a
// whole customer, because that is the read the delivery path makes and a second
// per-rule query would be a read per row of a list.
func forRule[T any](all []T, ruleID string, ruleOf func(T) string) []T {
	out := make([]T, 0, len(all))
	for _, item := range all {
		if ruleOf(item) == ruleID {
			out = append(out, item)
		}
	}
	return out
}
