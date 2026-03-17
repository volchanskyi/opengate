package api

import (
	"context"
	"errors"

	"github.com/volchanskyi/opengate/server/internal/db"
)

// GetMe implements StrictServerInterface.
func (s *Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	userID := ContextUserID(ctx)

	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return GetMe404JSONResponse{Error: "user not found"}, nil
		}
		return nil, err
	}

	return GetMe200JSONResponse(userToAPI(user)), nil
}

// ListUsers implements StrictServerInterface.
func (s *Server) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	if !isAdmin(ctx) {
		return ListUsers403JSONResponse{Error: "admin access required"}, nil
	}

	users, err := s.store.ListUsers(ctx)
	if err != nil {
		return nil, err
	}

	return ListUsers200JSONResponse(usersToAPI(users)), nil
}

// DeleteUser implements StrictServerInterface.
func (s *Server) DeleteUser(ctx context.Context, request DeleteUserRequestObject) (DeleteUserResponseObject, error) {
	if !isAdmin(ctx) {
		return DeleteUser403JSONResponse{Error: "admin access required"}, nil
	}

	if err := s.store.DeleteUser(ctx, request.Id); err != nil {
		return nil, err
	}

	s.auditLog(ContextUserID(ctx), "user.delete", request.Id.String(), "")

	return DeleteUser204Response{}, nil
}

// UpdateUser implements StrictServerInterface.
func (s *Server) UpdateUser(ctx context.Context, request UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	if !isAdmin(ctx) {
		return UpdateUser403JSONResponse{Error: "admin access required"}, nil
	}

	user, err := s.store.GetUser(ctx, request.Id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return UpdateUser404JSONResponse{Error: "user not found"}, nil
		}
		return nil, err
	}

	if request.Body.IsAdmin != nil {
		user.IsAdmin = *request.Body.IsAdmin
		// Sync Administrators group membership with is_admin flag.
		if *request.Body.IsAdmin {
			if err := s.store.AddSecurityGroupMember(ctx, db.AdminGroupID, user.ID); err != nil {
				return nil, err
			}
		} else {
			if err := s.store.RemoveSecurityGroupMember(ctx, db.AdminGroupID, user.ID); err != nil {
				if errors.Is(err, db.ErrLastAdmin) {
					return UpdateUser403JSONResponse{Error: "cannot remove last administrator"}, nil
				}
				// ErrNotFound is fine — user may not have been in the group.
				if !errors.Is(err, db.ErrNotFound) {
					return nil, err
				}
			}
		}
	}
	if request.Body.DisplayName != nil {
		user.DisplayName = *request.Body.DisplayName
	}

	if err := s.store.UpsertUser(ctx, user); err != nil {
		return nil, err
	}

	s.auditLog(ContextUserID(ctx), "user.update", user.Email, "")

	return UpdateUser200JSONResponse(userToAPI(user)), nil
}
