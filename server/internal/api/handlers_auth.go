package api

import (
	"context"
	"errors"
	"net/http"
	"net/mail"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// validateRegistration returns the accepted display name, or the problem with the request. The
// OpenAPI constraints are not enforced at runtime, so it bounds the request.
func validateRegistration(body *RegisterJSONRequestBody) (string, string) {
	email := string(body.Email)
	if email == "" || body.Password == "" {
		return "", "email and password are required"
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", "invalid email format"
	}
	if len(email) > maxEmailLen {
		return "", "invalid email format"
	}
	// bcrypt silently truncates beyond 72 bytes, so the upper bound is a
	// correctness requirement, not just a size cap.
	if len(body.Password) < 8 {
		return "", "password must be at least 8 characters"
	}
	if len(body.Password) > 72 {
		return "", "password must be at most 72 characters"
	}

	displayName := ""
	if body.DisplayName != nil {
		displayName = *body.DisplayName
	}
	if msg := invalidText("display_name", displayName, maxDisplayNameLen); msg != "" {
		return "", msg
	}
	return displayName, ""
}

// createAccount registers the user the body describes and reports whether it is an administrator;
// refused is the reason a request is turned away.
func (s *Server) createAccount(ctx context.Context, body *RegisterJSONRequestBody) (*auth.User, bool, string, error) {
	ctx = dbtx.WithDefaultTenant(ctx, false)
	email := string(body.Email)

	displayName, refused := validateRegistration(body)
	if refused != "" {
		return nil, false, refused, nil
	}

	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return nil, false, "", err
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
		return nil, false, "", lookupErr
	}
	if existing != nil {
		return nil, false, "registration failed", nil
	}

	if err := s.users.Upsert(ctx, user); err != nil {
		return nil, false, "", err
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
	s.auditLog(ctx, user.ID, "user.register", user.Email, "")
	return user, isAdmin, "", nil
}

// Register implements StrictServerInterface.
func (s *Server) Register(ctx context.Context, request RegisterRequestObject) (RegisterResponseObject, error) {
	user, isAdmin, refused, err := s.createAccount(ctx, request.Body)
	if err != nil {
		return nil, err
	}
	if refused != "" {
		return Register400JSONResponse{Error: refused}, nil
	}
	token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin, user.TenantID)
	if err != nil {
		return nil, err
	}
	return Register201JSONResponse{Token: token}, nil
}

// signInRefusal is why a sign-in was turned away: the status and the message the caller sees.
type signInRefusal struct {
	status int
	msg    string
}

// checkCredentials returns the user the email and password name and whether it is an
// administrator, or the refusal the caller answers with.
func (s *Server) checkCredentials(ctx context.Context, email, password string) (*auth.User, bool, *signInRefusal, error) {
	if email == "" || password == "" {
		return nil, false, &signInRefusal{http.StatusBadRequest, "email and password are required"}, nil
	}

	// The per-email lockout is checked before lookup and treats unknown and known emails alike,
	// which leaks no account enumeration signal.
	if !s.loginLimiter.allowed(email) {
		return nil, false, &signInRefusal{http.StatusTooManyRequests,
			"too many failed login attempts; try again later"}, nil
	}

	user, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, auth.ErrUserNotFound) {
			s.loginLimiter.recordFailure(email)
			return nil, false, &signInRefusal{http.StatusUnauthorized, "invalid credentials"}, nil
		}
		return nil, false, nil, err
	}

	if auth.CheckPassword(user.PasswordHash, password) != nil {
		s.loginLimiter.recordFailure(email)
		return nil, false, &signInRefusal{http.StatusUnauthorized, "invalid credentials"}, nil
	}

	s.loginLimiter.reset(email)

	tenantCtx := dbtx.WithTenant(ctx, user.TenantID, user.IsAdmin)
	isAdmin, adminErr := s.securityGroups.IsUserInGroup(tenantCtx, user.ID, auth.AdminGroupID)
	if adminErr != nil {
		s.logger.Warn("admin lookup failed", "user_id", user.ID, "error", adminErr)
	}
	s.auditLog(tenantCtx, user.ID, "user.login", user.Email, "")
	return user, isAdmin, nil, nil
}

// Login implements StrictServerInterface.
func (s *Server) Login(ctx context.Context, request LoginRequestObject) (LoginResponseObject, error) {
	user, isAdmin, refused, err := s.checkCredentials(ctx, string(request.Body.Email), request.Body.Password)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		switch refused.status {
		case http.StatusBadRequest:
			return Login400JSONResponse{Error: refused.msg}, nil
		case http.StatusTooManyRequests:
			return Login429JSONResponse{Error: refused.msg}, nil
		default:
			return Login401JSONResponse{Error: refused.msg}, nil
		}
	}
	token, err := s.jwt.GenerateToken(user.ID, user.Email, isAdmin, user.TenantID)
	if err != nil {
		return nil, err
	}
	return Login200JSONResponse{Token: token}, nil
}
