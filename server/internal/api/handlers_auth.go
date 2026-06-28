package api

import (
	"context"
	"errors"
	"net/mail"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
)

// Register implements StrictServerInterface.
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	email := string(request.Body.Email)
	if email == "" || request.Body.Password == "" {
		return Register400JSONResponse{Error: "email and password are required"}, nil
	}

	if _, err := mail.ParseAddress(email); err != nil {
		return Register400JSONResponse{Error: "invalid email format"}, nil
	}

	if len(request.Body.Password) < 8 {
		return Register400JSONResponse{Error: "password must be at least 8 characters"}, nil
	}
	if len(request.Body.Password) > 72 {
		return Register400JSONResponse{Error: "password must be at most 72 characters"}, nil
	}

	hash, err := auth.HashPassword(request.Body.Password)
	if err != nil {
		return nil, err
	}

	displayName := ""
	if request.Body.DisplayName != nil {
		displayName = *request.Body.DisplayName
	}

	user := &auth.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
	}

	// Check for duplicate email to prevent account enumeration.
	existing, lookupErr := s.users.GetByEmail(ctx, email)
	if lookupErr != nil && !errors.Is(lookupErr, auth.ErrUserNotFound) {
		return nil, lookupErr
	}
	if existing != nil {
		return Register400JSONResponse{Error: "registration failed"}, nil
	}

	if err := s.users.Upsert(ctx, user); err != nil {
		return nil, err
	}

	// Auto-promote the first registered user to administrator.
	users, err := s.users.List(ctx)
	if err == nil && len(users) == 1 {
		if addErr := s.securityGroups.AddMember(ctx, auth.AdminGroupID, user.ID); addErr != nil {
			s.logger.Warn("auto-promote first user to admin failed", "user_id", user.ID, "error", addErr)
		}
	}

	isAdmin, adminErr := s.securityGroups.IsUserInGroup(ctx, user.ID, auth.AdminGroupID)
	if adminErr != nil {
		s.logger.Warn("admin lookup failed", "user_id", user.ID, "error", adminErr)
	}
	token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin)
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

	// Per-email lockout, independent of source IP. Checked before lookup and
	// applied identically to unknown and known emails so it adds no account
	// enumeration signal.
	if !s.loginLimiter.allowed(email) {
		return Login429JSONResponse{Error: "too many failed login attempts; try again later"}, nil
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			s.loginLimiter.recordFailure(email)
			return Login401JSONResponse{Error: "invalid credentials"}, nil
		}
		return nil, err
	}

	if auth.CheckPassword(user.PasswordHash, request.Body.Password) != nil {
		s.loginLimiter.recordFailure(email)
		return Login401JSONResponse{Error: "invalid credentials"}, nil
	}

	s.loginLimiter.reset(email)

	isAdmin, adminErr := s.securityGroups.IsUserInGroup(ctx, user.ID, auth.AdminGroupID)
	if adminErr != nil {
		s.logger.Warn("admin lookup failed", "user_id", user.ID, "error", adminErr)
	}
	token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin)
	if err != nil {
		return nil, err
	}

	s.auditLog(user.ID, "user.login", user.Email, "")

	return Login200JSONResponse{Token: token}, nil
}
