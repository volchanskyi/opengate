package api

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// The moves a person makes on an incident: where it stands, who is working it,
// and what they want the next technician to know.
//
// All three are operational work on the tenant's own resources, so tenant
// membership is the whole gate — the same rule a restart or a maintenance
// window follows. Configuration is what is admin-gated, and a rule's bindings
// and rollout are configuration; a triage queue is not.
//
// Each refusal below is its own answer because each is a different mistake with
// a different fix: a move the lifecycle does not allow, a resolution with no
// answer for why, a code outside the closed set. Collapsing them into one
// rejection would leave a technician guessing which of the three they made.

// msgIncidentNotFound is the one answer every boundary gives, so a caller
// cannot tell a room they may not see from one that does not exist.
const msgIncidentNotFound = "incident not found"

// msgAssigneeNotFound is the same answer for a person who is not in the
// caller's tenant.
const msgAssigneeNotFound = "assignee not found"

// SetInvestigationStatus implements StrictServerInterface. It moves an incident
// through its lifecycle and records who moved it.
func (s *Server) SetInvestigationStatus(ctx context.Context, request SetInvestigationStatusRequestObject) (SetInvestigationStatusResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return SetInvestigationStatus404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	change := alerts.Change{
		To:    alerts.Status(request.Body.Status),
		Actor: ContextUserID(ctx),
	}
	if request.Body.CauseCode != nil {
		change.Cause = alerts.CauseCode(*request.Body.CauseCode)
	}

	switch err := s.investigations.Transition(ctx, request.Id, change); {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return SetInvestigationStatus404JSONResponse{Error: msgIncidentNotFound}, nil
	case isRefusedMove(err):
		return SetInvestigationStatus400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	moved, err := s.investigations.Incident(ctx, request.Id, uuid.Nil)
	if err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "incident.status", request.Id.String(), string(change.To))
	return SetInvestigationStatus200JSONResponse(incidentToAPI(moved)), nil
}

// isRefusedMove reports whether the store refused a transition because of what
// was asked for, rather than because something failed. Each of these is a
// caller's mistake, and the error itself names which one.
func isRefusedMove(err error) bool {
	return errors.Is(err, alerts.ErrIllegalTransition) ||
		errors.Is(err, alerts.ErrUnknownStatus) ||
		errors.Is(err, alerts.ErrUnknownCause) ||
		errors.Is(err, alerts.ErrCauseRequired) ||
		errors.Is(err, alerts.ErrCauseNotAllowed)
}

// SetInvestigationAssignee implements StrictServerInterface. An absent assignee
// hands the incident back to the queue, which is the move a technician going off
// shift makes rather than leaving a room looking worked.
func (s *Server) SetInvestigationAssignee(ctx context.Context, request SetInvestigationAssigneeRequestObject) (SetInvestigationAssigneeResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return SetInvestigationAssignee404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	var assignee uuid.UUID
	if request.Body.AssigneeId != nil {
		assignee = *request.Body.AssigneeId
		// Resolved through the tenant-scoped user read, so a name from outside
		// the tenant answers the same as one that does not exist. Without this an
		// incident could be handed to somebody who cannot see it.
		if _, err := s.users.Get(ctx, assignee); err != nil {
			if errors.Is(err, auth.ErrUserNotFound) {
				return SetInvestigationAssignee404JSONResponse{Error: msgAssigneeNotFound}, nil
			}
			return nil, err
		}
	}

	switch err := s.investigations.Assign(ctx, request.Id, assignee, ContextUserID(ctx)); {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return SetInvestigationAssignee404JSONResponse{Error: msgIncidentNotFound}, nil
	default:
		return nil, err
	}

	taken, err := s.investigations.Incident(ctx, request.Id, uuid.Nil)
	if err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "incident.assign", request.Id.String(), assignee.String())
	return SetInvestigationAssignee200JSONResponse(incidentToAPI(taken)), nil
}

// AddInvestigationComment implements StrictServerInterface. A comment is one
// more thing that happened, in the order it happened, so it lands in the same
// append-only history a status change does rather than as a field on the
// incident.
//
// It is not written to the audit log: the line it produces is already the
// record, and copying a technician's prose into a second store would put the
// same words in two places with two retentions.
func (s *Server) AddInvestigationComment(ctx context.Context, request AddInvestigationCommentRequestObject) (AddInvestigationCommentResponseObject, error) {
	if _, err := s.requireIncidentInScope(ctx, request.Id, deref(request.Params.OrganizationId)); err != nil {
		if errors.Is(err, alerts.ErrIncidentNotFound) {
			return AddInvestigationComment404JSONResponse{Error: msgIncidentNotFound}, nil
		}
		return nil, err
	}

	event, err := s.investigations.Comment(ctx, request.Id, ContextUserID(ctx), request.Body.Body)
	switch {
	case err == nil:
	case errors.Is(err, alerts.ErrIncidentNotFound):
		return AddInvestigationComment404JSONResponse{Error: msgIncidentNotFound}, nil
	case errors.Is(err, alerts.ErrCommentUnusable):
		return AddInvestigationComment400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}
	return AddInvestigationComment201JSONResponse(incidentEventToAPI(event)), nil
}
