package api

import (
	"context"
	"net/http"
)

// SignIn implements StrictServerInterface.
func (s *Server) SignIn(ctx context.Context, request SignInRequestObject) (SignInResponseObject, error) {
	user, isAdmin, refused, err := s.checkCredentials(ctx, string(request.Body.Email), request.Body.Password)
	if err != nil {
		return nil, err
	}
	if refused != nil {
		switch refused.status {
		case http.StatusBadRequest:
			return SignIn400JSONResponse{Error: refused.msg}, nil
		case http.StatusTooManyRequests:
			return SignIn429JSONResponse{Error: refused.msg}, nil
		default:
			return SignIn401JSONResponse{Error: refused.msg}, nil
		}
	}
	cookie, body, err := s.startConsoleSession(ctx, user, isAdmin)
	if err != nil {
		return nil, err
	}
	return SignIn200JSONResponse{Body: body, Headers: SignIn200ResponseHeaders{SetCookie: cookie.String()}}, nil
}

// RegisterConsole implements StrictServerInterface.
func (s *Server) RegisterConsole(ctx context.Context, request RegisterConsoleRequestObject) (RegisterConsoleResponseObject, error) {
	user, isAdmin, refused, err := s.createAccount(ctx, request.Body)
	if err != nil {
		return nil, err
	}
	if refused != "" {
		return RegisterConsole400JSONResponse{Error: refused}, nil
	}
	cookie, body, err := s.startConsoleSession(ctx, user, isAdmin)
	if err != nil {
		return nil, err
	}
	return RegisterConsole201JSONResponse{Body: body,
		Headers: RegisterConsole201ResponseHeaders{SetCookie: cookie.String()}}, nil
}

// GetConsoleSession implements StrictServerInterface.
func (s *Server) GetConsoleSession(ctx context.Context, _ GetConsoleSessionRequestObject) (GetConsoleSessionResponseObject, error) {
	session := contextConsoleSession(ctx)
	if session == nil {
		return GetConsoleSession401JSONResponse{Error: "no console session"}, nil
	}
	user, err := s.users.Get(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	user.IsAdmin = session.IsAdmin
	return GetConsoleSession200JSONResponse(consoleSessionBody(user, session)), nil
}

// SignOut implements StrictServerInterface.
func (s *Server) SignOut(ctx context.Context, _ SignOutRequestObject) (SignOutResponseObject, error) {
	session := contextConsoleSession(ctx)
	if session == nil {
		return SignOut401JSONResponse{Error: "no console session"}, nil
	}
	if err := s.consoleSessions.Delete(ctx, session.IDHash); err != nil {
		return nil, err
	}
	s.auditLog(ctx, session.UserID, "user.logout", session.Email, "")
	cleared := consoleCookie("", -1, reachedOverTLS(httpRequestFromContext(ctx)))
	return SignOut204Response{Headers: SignOut204ResponseHeaders{SetCookie: cleared.String()}}, nil
}
