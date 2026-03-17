package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// ListSecurityGroups implements StrictServerInterface.
func (s *Server) ListSecurityGroups(ctx context.Context, _ ListSecurityGroupsRequestObject) (ListSecurityGroupsResponseObject, error) {
	if !isAdmin(ctx) {
		return ListSecurityGroups403JSONResponse{Error: "admin access required"}, nil
	}
	groups, err := s.store.ListSecurityGroups(ctx)
	if err != nil {
		return nil, err
	}
	result := make(ListSecurityGroups200JSONResponse, 0, len(groups))
	for _, g := range groups {
		result = append(result, securityGroupToAPI(g))
	}
	return result, nil
}

// CreateSecurityGroup implements StrictServerInterface.
func (s *Server) CreateSecurityGroup(ctx context.Context, request CreateSecurityGroupRequestObject) (CreateSecurityGroupResponseObject, error) {
	if !isAdmin(ctx) {
		return CreateSecurityGroup403JSONResponse{Error: "admin access required"}, nil
	}
	if request.Body.Name == "" {
		return CreateSecurityGroup400JSONResponse{Error: "name is required"}, nil
	}
	desc := ""
	if request.Body.Description != nil {
		desc = *request.Body.Description
	}
	g := &db.SecurityGroup{
		ID:          uuid.New(),
		Name:        request.Body.Name,
		Description: desc,
	}
	if err := s.store.CreateSecurityGroup(ctx, g); err != nil {
		return nil, err
	}
	s.auditLog(ContextUserID(ctx), "security_group.create", g.ID.String(), g.Name)
	created, err := s.store.GetSecurityGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	return CreateSecurityGroup201JSONResponse(securityGroupToAPI(created)), nil
}

// GetSecurityGroup implements StrictServerInterface.
func (s *Server) GetSecurityGroup(ctx context.Context, request GetSecurityGroupRequestObject) (GetSecurityGroupResponseObject, error) {
	if !isAdmin(ctx) {
		return GetSecurityGroup403JSONResponse{Error: "admin access required"}, nil
	}
	g, err := s.store.GetSecurityGroup(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetSecurityGroup404JSONResponse{Error: "security group not found"}, nil
		}
		return nil, err
	}
	members, err := s.store.ListSecurityGroupMembers(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	apiMembers := make([]User, 0, len(members))
	for _, m := range members {
		apiMembers = append(apiMembers, userToAPI(m))
	}
	return GetSecurityGroup200JSONResponse{
		Id:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		IsSystem:    g.IsSystem,
		Members:     apiMembers,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}, nil
}

// DeleteSecurityGroup implements StrictServerInterface.
func (s *Server) DeleteSecurityGroup(ctx context.Context, request DeleteSecurityGroupRequestObject) (DeleteSecurityGroupResponseObject, error) {
	if !isAdmin(ctx) {
		return DeleteSecurityGroup403JSONResponse{Error: "admin access required"}, nil
	}
	err := s.store.DeleteSecurityGroup(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return DeleteSecurityGroup404JSONResponse{Error: "security group not found"}, nil
		}
		if errors.Is(err, db.ErrSystemGroup) {
			return DeleteSecurityGroup403JSONResponse{Error: "cannot delete system group"}, nil
		}
		return nil, err
	}
	s.auditLog(ContextUserID(ctx), "security_group.delete", request.Id.String(), "")
	return DeleteSecurityGroup204Response{}, nil
}

// AddSecurityGroupMember implements StrictServerInterface.
func (s *Server) AddSecurityGroupMember(ctx context.Context, request AddSecurityGroupMemberRequestObject) (AddSecurityGroupMemberResponseObject, error) {
	if !isAdmin(ctx) {
		return AddSecurityGroupMember403JSONResponse{Error: "admin access required"}, nil
	}
	// Verify group exists.
	if _, err := s.store.GetSecurityGroup(ctx, request.Id); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return AddSecurityGroupMember404JSONResponse{Error: "security group not found"}, nil
		}
		return nil, err
	}
	// Verify user exists.
	if _, err := s.store.GetUser(ctx, request.Body.UserId); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return AddSecurityGroupMember404JSONResponse{Error: "user not found"}, nil
		}
		return nil, err
	}
	if err := s.store.AddSecurityGroupMember(ctx, request.Id, request.Body.UserId); err != nil {
		return nil, err
	}
	s.auditLog(ContextUserID(ctx), "security_group.add_member", request.Id.String(), request.Body.UserId.String())
	return AddSecurityGroupMember204Response{}, nil
}

// RemoveSecurityGroupMember implements StrictServerInterface.
func (s *Server) RemoveSecurityGroupMember(ctx context.Context, request RemoveSecurityGroupMemberRequestObject) (RemoveSecurityGroupMemberResponseObject, error) {
	if !isAdmin(ctx) {
		return RemoveSecurityGroupMember403JSONResponse{Error: "admin access required"}, nil
	}
	err := s.store.RemoveSecurityGroupMember(ctx, request.Id, request.UserId)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return RemoveSecurityGroupMember404JSONResponse{Error: "member not found"}, nil
		}
		if errors.Is(err, db.ErrLastAdmin) {
			return RemoveSecurityGroupMember409JSONResponse{Error: "cannot remove last administrator"}, nil
		}
		return nil, err
	}
	s.auditLog(ContextUserID(ctx), "security_group.remove_member", request.Id.String(), request.UserId.String())
	return RemoveSecurityGroupMember204Response{}, nil
}

func securityGroupToAPI(g *db.SecurityGroup) SecurityGroup {
	return SecurityGroup{
		Id:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		IsSystem:    g.IsSystem,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}
}
