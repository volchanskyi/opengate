package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// CreateGroup implements StrictServerInterface.
func (s *Server) CreateGroup(ctx context.Context, request CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	if request.Body.Name == "" {
		return CreateGroup400JSONResponse{Error: "name is required"}, nil
	}

	group := &db.Group{
		ID:      uuid.New(),
		Name:    request.Body.Name,
		OwnerID: ContextUserID(ctx),
	}

	if err := s.store.CreateGroup(ctx, group); err != nil {
		return nil, err
	}

	return CreateGroup201JSONResponse(groupToAPI(group)), nil
}

// ListGroups implements StrictServerInterface.
func (s *Server) ListGroups(ctx context.Context, _ ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	groups, err := s.store.ListGroups(ctx, ContextUserID(ctx))
	if err != nil {
		return nil, err
	}

	return ListGroups200JSONResponse(groupsToAPI(groups)), nil
}

// GetGroup implements StrictServerInterface.
func (s *Server) GetGroup(ctx context.Context, request GetGroupRequestObject) (GetGroupResponseObject, error) {
	group, err := s.store.GetGroup(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetGroup404JSONResponse{Error: "group not found"}, nil
		}
		return nil, err
	}

	return GetGroup200JSONResponse(groupToAPI(group)), nil
}

// DeleteGroup implements StrictServerInterface.
func (s *Server) DeleteGroup(ctx context.Context, request DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	if err := s.store.DeleteGroup(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteGroup204Response{}, nil
}
