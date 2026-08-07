package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/device"
)

const msgGroupNotFound = "group not found"

// CreateGroup implements StrictServerInterface. Adding a group reshapes how the
// fleet is filed, so it sits behind the admin gate.
func (s *Server) CreateGroup(ctx context.Context, request CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, CreateGroup403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if request.Body.Name == "" {
		return CreateGroup400JSONResponse{Error: "name is required"}, nil
	}
	if msg := invalidText("name", request.Body.Name, maxGroupNameLen); msg != "" {
		return CreateGroup400JSONResponse{Error: msg}, nil
	}

	group := &device.Group{
		ID:   uuid.New(),
		Name: request.Body.Name,
	}

	if err := s.groups.Create(ctx, group); err != nil {
		return nil, err
	}

	return CreateGroup201JSONResponse(groupToAPI(group)), nil
}

// ListGroups implements StrictServerInterface. Groups are a fleet read: every
// member of the tenant sees every group in it.
func (s *Server) ListGroups(ctx context.Context, _ ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	groups, err := s.groups.List(ctx)
	if err != nil {
		return nil, err
	}

	return ListGroups200JSONResponse(groupsToAPI(groups)), nil
}

// GetGroup implements StrictServerInterface.
func (s *Server) GetGroup(ctx context.Context, request GetGroupRequestObject) (GetGroupResponseObject, error) {
	group, err := s.groups.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, device.ErrGroupNotFound) {
			return GetGroup404JSONResponse{Error: msgGroupNotFound}, nil
		}
		return nil, err
	}

	return GetGroup200JSONResponse(groupToAPI(group)), nil
}

// DeleteGroup implements StrictServerInterface. Removing a group reshapes how
// the fleet is filed, so it sits behind the admin gate.
func (s *Server) DeleteGroup(ctx context.Context, request DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, DeleteGroup403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}

	if _, err := s.groups.Get(ctx, request.Id); err != nil {
		if errors.Is(err, device.ErrGroupNotFound) {
			return DeleteGroup404JSONResponse{Error: msgGroupNotFound}, nil
		}
		return nil, err
	}

	if err := s.groups.Delete(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteGroup204Response{}, nil
}
