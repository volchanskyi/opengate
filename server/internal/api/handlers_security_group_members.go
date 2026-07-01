package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/auth"
)

// AddSecurityGroupMember implements StrictServerInterface.
func (s *Server) AddSecurityGroupMember(ctx context.Context, request AddSecurityGroupMemberRequestObject) (AddSecurityGroupMemberResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, AddSecurityGroupMember403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	if _, err := s.securityGroups.Get(ctx, request.Id); err != nil {
		if errors.Is(err, auth.ErrSecurityGroupNotFound) {
			return AddSecurityGroupMember404JSONResponse{Error: msgSecurityGroupNotFound}, nil
		}
		return nil, err
	}
	if _, err := s.users.Get(ctx, request.Body.UserId); err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			return AddSecurityGroupMember404JSONResponse{Error: "user not found"}, nil
		}
		return nil, err
	}
	if err := s.securityGroups.AddMember(ctx, request.Id, request.Body.UserId); err != nil {
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "security_group.add_member", request.Id.String(), request.Body.UserId.String())
	return AddSecurityGroupMember204Response{}, nil
}

// RemoveSecurityGroupMember implements StrictServerInterface.
func (s *Server) RemoveSecurityGroupMember(ctx context.Context, request RemoveSecurityGroupMemberRequestObject) (RemoveSecurityGroupMemberResponseObject, error) {
	if resp, denied := denyIfNotAdmin(ctx, RemoveSecurityGroupMember403JSONResponse{Error: msgAdminRequired}); denied {
		return resp, nil
	}
	err := s.securityGroups.RemoveMember(ctx, request.Id, request.UserId)
	if err != nil {
		if errors.Is(err, auth.ErrMemberNotFound) {
			return RemoveSecurityGroupMember404JSONResponse{Error: "member not found"}, nil
		}
		if errors.Is(err, auth.ErrLastAdmin) {
			return RemoveSecurityGroupMember409JSONResponse{Error: "cannot remove last administrator"}, nil
		}
		return nil, err
	}
	s.auditLog(ctx, ContextUserID(ctx), "security_group.remove_member", request.Id.String(), request.UserId.String())
	return RemoveSecurityGroupMember204Response{}, nil
}
