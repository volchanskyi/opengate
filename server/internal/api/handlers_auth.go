package api

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// Register implements StrictServerInterface.
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	email := string(request.Body.Email)
	if email == "" || request.Body.Password == "" {
		return Register400JSONResponse{Error: "email and password are required"}, nil
	}

	hash, err := auth.HashPassword(request.Body.Password)
	if err != nil {
		return nil, err
	}

	displayName := ""
	if request.Body.DisplayName != nil {
		displayName = *request.Body.DisplayName
	}

	user := &db.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	}

	if err := s.store.UpsertUser(ctx, user); err != nil {
		return nil, err
	}

	token, err := s.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	if err != nil {
		return nil, err
	}

	s.auditLog(user.ID, "user.register", user.Email, "")

	return Register201JSONResponse{Token: token}, nil
}

// Login implements StrictServerInterface.
func (s *Server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	email := string(request.Body.Email)
	if email == "" || request.Body.Password == "" {
		return Login400JSONResponse{Error: "email and password are required"}, nil
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return Login401JSONResponse{Error: "invalid credentials"}, nil
		}
		return nil, err
	}

	if auth.CheckPassword(user.PasswordHash, request.Body.Password) != nil {
		return Login401JSONResponse{Error: "invalid credentials"}, nil
	}

	token, err := s.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	if err != nil {
		return nil, err
	}

	s.auditLog(user.ID, "user.login", user.Email, "")

	return Login200JSONResponse{Token: token}, nil
}
