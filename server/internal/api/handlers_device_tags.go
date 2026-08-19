package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// The labels a customer's machines are picked out by.
//
// Labels cut across the tenancy ladder rather than sitting on a rung of it: a
// disk threshold is meant for the file servers, and the file servers are in four
// offices. No rung names that set. The values come from a list rather than being
// typed in, because a targeting dimension with free-text values is one where
// `production`, `Production` and `prod` are three estates and a threshold
// reaches a third of the machines it was meant for.

// errTagsNotConfigured is a deployment wired without the label store.
var errTagsNotConfigured = errors.New("device labels are not configured on this server")

// ListDeviceTags implements StrictServerInterface. Every member of the tenant
// reads the list: it is what explains why a machine has the thresholds it has.
func (s *Server) ListDeviceTags(ctx context.Context, request ListDeviceTagsRequestObject) (ListDeviceTagsResponseObject, error) {
	if s.ruleAdmin == nil {
		return nil, errTagsNotConfigured
	}
	organizationID := deref(request.Params.OrganizationId)

	labels, err := s.ruleAdmin.ListLabels(ctx, organizationID)
	if err != nil {
		return nil, err
	}
	assignments, err := s.ruleAdmin.ListTagAssignments(ctx, organizationID)
	if err != nil {
		return nil, err
	}

	return ListDeviceTags200JSONResponse(DeviceTagCatalogue{
		Labels:      labelsToAPI(labels),
		Assignments: assignmentsToAPI(assignments),
	}), nil
}

// CreateDeviceTagLabel implements StrictServerInterface.
func (s *Server) CreateDeviceTagLabel(ctx context.Context, request CreateDeviceTagLabelRequestObject) (CreateDeviceTagLabelResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateDeviceTagLabel403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errTagsNotConfigured
	}

	organizationID, err := s.customerOrDefault(ctx, request.Params.OrganizationId)
	if err != nil {
		return nil, err
	}
	label := rules.Label{
		ID:             uuid.New(),
		OrganizationID: organizationID,
		Key:            request.Body.Key,
		Value:          request.Body.Value,
		CreatedBy:      ContextUserID(ctx).String(),
	}

	switch err := s.ruleAdmin.CreateLabel(ctx, label); {
	case err == nil:
	case errors.Is(err, rules.ErrInvalidLabel), errors.Is(err, rules.ErrLabelExists):
		return CreateDeviceTagLabel400JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.tag.label.create", label.ID.String(),
		fmt.Sprintf("%s=%s", label.Key, label.Value))
	return CreateDeviceTagLabel201JSONResponse(labelToAPI(label)), nil
}

// DeleteDeviceTagLabel implements StrictServerInterface.
//
// Refused while a rule is aimed at the label. Removing it then would take a
// tuned value off every machine that carried it — which does not read as a
// deletion at all, it reads as a threshold that quietly widened across an estate
// one afternoon.
func (s *Server) DeleteDeviceTagLabel(ctx context.Context, request DeleteDeviceTagLabelRequestObject) (DeleteDeviceTagLabelResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteDeviceTagLabel403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errTagsNotConfigured
	}

	switch err := s.ruleAdmin.DeleteLabel(ctx, request.LabelId); {
	case err == nil:
	case errors.Is(err, rules.ErrLabelNotFound):
		return DeleteDeviceTagLabel404JSONResponse{Error: err.Error()}, nil
	case errors.Is(err, rules.ErrLabelInUse):
		return DeleteDeviceTagLabel409JSONResponse{Error: err.Error()}, nil
	default:
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.tag.label.delete", request.LabelId.String(), "")
	return DeleteDeviceTagLabel204Response{}, nil
}

// AssignDeviceTag implements StrictServerInterface. Bulk by design: labelling an
// estate one machine's page at a time is how a targeting dimension ends up
// half-applied.
func (s *Server) AssignDeviceTag(ctx context.Context, request AssignDeviceTagRequestObject) (AssignDeviceTagResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, AssignDeviceTag403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errTagsNotConfigured
	}

	actor := ContextUserID(ctx).String()
	for _, deviceID := range request.Body.DeviceIds {
		switch err := s.ruleAdmin.AssignTag(ctx, deviceID, request.Body.LabelId, actor); {
		case err == nil:
		case errors.Is(err, rules.ErrLabelForeign):
			return AssignDeviceTag400JSONResponse{Error: err.Error()}, nil
		default:
			return nil, err
		}
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.tag.assign", request.Body.LabelId.String(),
		fmt.Sprintf("machines=%d", len(request.Body.DeviceIds)))
	return AssignDeviceTag204Response{}, nil
}

// ClearDeviceTag implements StrictServerInterface.
func (s *Server) ClearDeviceTag(ctx context.Context, request ClearDeviceTagRequestObject) (ClearDeviceTagResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, ClearDeviceTag403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if s.ruleAdmin == nil {
		return nil, errTagsNotConfigured
	}
	if err := s.ruleAdmin.ClearTag(ctx, request.Params.DeviceId, request.Params.Key); err != nil {
		return nil, err
	}

	s.auditLog(ctx, ContextUserID(ctx), "device.tag.clear", request.Params.DeviceId.String(),
		request.Params.Key)
	return ClearDeviceTag204Response{}, nil
}
