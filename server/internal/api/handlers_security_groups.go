package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// ListSecurityGroups implements StrictServerInterface.
func (s *Server) ListSecurityGroups(ctx context.Context, _ ListSecurityGroupsRequestObject) (ListSecurityGroupsResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, ListSecurityGroups403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	groups, err := s.securityGroups.List(ctx)
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
	if resp, denied := denyIfNotAdmin(ctx, CreateSecurityGroup403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if request.Body.Name == "" {
		return CreateSecurityGroup400JSONResponse{Error: "name is required"}, nil
	}
	desc := ""
	if request.Body.Description != nil {
		desc = *request.Body.Description
	}
	g := &auth.SecurityGroup{
		ID:          uuid.New(),
		Name:        request.Body.Name,
		Description: desc,
	}
	if err := s.securityGroups.Create(ctx, g); err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "security_group.create", g.ID.String(), g.Name)
	created, err := s.securityGroups.Get(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	return CreateSecurityGroup201JSONResponse(securityGroupToAPI(created)), nil
}

// GetSecurityGroup implements StrictServerInterface.
func (s *Server) GetSecurityGroup(ctx context.Context, request GetSecurityGroupRequestObject) (GetSecurityGroupResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, GetSecurityGroup403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	g, err := s.securityGroups.Get(ctx, request.Id)
	if err != nil {
		if errors.Is(err, auth.ErrSecurityGroupNotFound) {
			return GetSecurityGroup404JSONResponse{Error: msgSecurityGroupNotFound}, nil
		}
		return nil, err
	}
	members, err := s.securityGroups.ListMembers(ctx, request.Id)
	if err != nil {
		return nil, err
	}
	apiMembers := make([]User, 0, len(members))
	for _, m := range members {
		apiMembers = append(apiMembers, memberToAPI(m))
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
	if resp, denied := denyIfNotAdmin(ctx, DeleteSecurityGroup403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	err := s.securityGroups.Delete(ctx, request.Id)
	if err != nil {
		if errors.Is(err, auth.ErrSecurityGroupNotFound) {
			return DeleteSecurityGroup404JSONResponse{Error: msgSecurityGroupNotFound}, nil
		}
		if errors.Is(err, auth.ErrSystemGroup) {
			return DeleteSecurityGroup403JSONResponse{Error: "cannot delete system group"}, nil
		}
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "security_group.delete", request.Id.String(), "")
	return DeleteSecurityGroup204Response{}, nil
}

func securityGroupToAPI(g *auth.SecurityGroup) SecurityGroup {
	return SecurityGroup{
		Id:          g.ID,
		Name:        g.Name,
		Description: g.Description,
		IsSystem:    g.IsSystem,
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}
}

func memberToAPI(m *auth.Member) User {
	return User{
		Id:          m.ID,
		Email:       m.Email,
		DisplayName: m.DisplayName,
		IsAdmin:     m.IsAdmin,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}
