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
	claims := ContextClaims(ctx)
	if claims == nil || !claims.IsAdmin {
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
	claims := ContextClaims(ctx)
	if claims == nil || !claims.IsAdmin {
		return DeleteUser403JSONResponse{Error: "admin access required"}, nil
	}

	if err := s.store.DeleteUser(ctx, request.Id); err != nil {
		return nil, err
	}

	return DeleteUser204Response{}, nil
}
