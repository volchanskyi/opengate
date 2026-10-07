package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/rules"
)

// Labels pick out a customer's machines across the tenancy ladder; their values come from a
// managed list because free-text values would split one estate into several.

// errTagsNotConfigured is a deployment wired without the label store.
var errTagsNotConfigured = errors.New("device labels are not configured on this server")

// ListDeviceTags implements StrictServerInterface; every member of the tenant can read the list.
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

// DeleteDeviceTagLabel implements StrictServerInterface and refuses while a rule targets the
// label, since removal would silently widen that rule's threshold.
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

// AssignDeviceTag implements StrictServerInterface and labels devices in bulk.
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
