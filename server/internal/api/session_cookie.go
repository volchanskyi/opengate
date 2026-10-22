package api

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const (
	// consoleCookieName names the cookie carrying a console sign-in.
	consoleCookieName = "og_session"
	// csrfHeader carries the forged-request value on every request that changes something.
	csrfHeader = "X-CSRF-Token"
	// consoleTouchEvery bounds how often requests write a session's last-seen time.
	consoleTouchEvery = time.Minute
	// sessionIDBytes is the random session id's length; the database keeps only its hash.
	sessionIDBytes = 32
	// defaultLiveSessionRecheck is how often an open live connection's console session is checked.
	defaultLiveSessionRecheck = 30 * time.Minute
)

// errNoConsoleSession is a request whose cookie names no live console session.
var errNoConsoleSession = errors.New("no live console session")

type consoleSessionKey struct{}

// contextConsoleSession returns the console session a cookie-authenticated request runs under.
func contextConsoleSession(ctx context.Context) *auth.ConsoleSession {
	session, _ := ctx.Value(consoleSessionKey{}).(*auth.ConsoleSession)
	return session
}

// cookieKey derives the key sealing session cookies from the server's signing secret.
func (s *Server) cookieKey() []byte {
	mac := hmac.New(sha256.New, []byte(s.jwt.Secret))
	mac.Write([]byte("og_session cookie"))
	return mac.Sum(nil)
}

// sealSessionID writes the session id beside an HMAC of it, so a forged cookie is refused before
// any database read.
func (s *Server) sealSessionID(id []byte) string {
	mac := hmac.New(sha256.New, s.cookieKey())
	mac.Write(id)
	return base64.RawURLEncoding.EncodeToString(id) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// openSessionCookie returns the session id a sealed cookie carries, or false for a forged one.
func (s *Server) openSessionCookie(value string) ([]byte, bool) {
	idPart, macPart, found := strings.Cut(value, ".")
	if !found {
		return nil, false
	}
	id, err := base64.RawURLEncoding.DecodeString(idPart)
	if err != nil || len(id) != sessionIDBytes {
		return nil, false
	}
	got, err := base64.RawURLEncoding.DecodeString(macPart)
	if err != nil {
		return nil, false
	}
	mac := hmac.New(sha256.New, s.cookieKey())
	mac.Write(id)
	return id, hmac.Equal(got, mac.Sum(nil))
}

// consoleCookie is the og_session cookie carrying value; a negative maxAge clears it. It is Secure
// whenever the browser reached the console over TLS, which every deployed edge serves.
func consoleCookie(value string, maxAge int, secure bool) *http.Cookie {
	// #nosec G124 -- Secure follows the scheme the browser used; every deployed edge serves HTTPS.
	return &http.Cookie{
		Name:     consoleCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

// reachedOverTLS reports whether the browser reached the console over TLS: at the edge, which
// names the scheme in X-Forwarded-Proto, or at the server itself. A request it cannot see counts.
func reachedOverTLS(r *http.Request) bool {
	if r == nil {
		return true
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto != "http"
	}
	return r.TLS != nil
}

func randomToken(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	return b, err
}

// startConsoleSession records a sign-in for user and returns its cookie and the page's body.
func (s *Server) startConsoleSession(ctx context.Context, user *auth.User, isAdmin bool) (*http.Cookie, ConsoleSession, error) {
	id, err := randomToken(sessionIDBytes)
	if err != nil {
		return nil, ConsoleSession{}, err
	}
	csrf, err := randomToken(32)
	if err != nil {
		return nil, ConsoleSession{}, err
	}
	now := s.now()
	session := &auth.ConsoleSession{
		IDHash:     sha256.Sum256(id),
		UserID:     user.ID,
		TenantID:   user.TenantID,
		Email:      user.Email,
		IsAdmin:    isAdmin,
		CSRFToken:  base64.RawURLEncoding.EncodeToString(csrf),
		StartedAt:  now,
		LastSeenAt: now,
	}
	if err := s.consoleSessions.Create(ctx, session); err != nil {
		return nil, ConsoleSession{}, err
	}
	user.IsAdmin = isAdmin
	cookie := consoleCookie(s.sealSessionID(id), int(auth.ConsoleTotalLimit/time.Second),
		reachedOverTLS(httpRequestFromContext(ctx)))
	return cookie, consoleSessionBody(user, session), nil
}

func consoleSessionBody(user *auth.User, session *auth.ConsoleSession) ConsoleSession {
	return ConsoleSession{
		User: User{
			Id:          openapi_types.UUID(user.ID),
			Email:       user.Email,
			DisplayName: user.DisplayName,
			IsAdmin:     user.IsAdmin,
			CreatedAt:   user.CreatedAt,
			UpdatedAt:   user.UpdatedAt,
		},
		TenantId:           openapi_types.UUID(session.TenantID),
		CsrfToken:          session.CSRFToken,
		IdleTimeoutSeconds: int(auth.ConsoleIdleLimit / time.Second),
		ExpiresAt:          session.StartedAt.Add(auth.ConsoleTotalLimit),
	}
}

// liveConsoleSession resolves a cookie value to its session, ending one past either limit.
func (s *Server) liveConsoleSession(ctx context.Context, value string, idleCounts bool) (*auth.ConsoleSession, error) {
	id, ok := s.openSessionCookie(value)
	if !ok {
		return nil, errNoConsoleSession
	}
	session, err := s.consoleSessions.Get(ctx, sha256.Sum256(id))
	if errors.Is(err, auth.ErrConsoleSessionNotFound) {
		return nil, errNoConsoleSession
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	idle := idleCounts && now.Sub(session.LastSeenAt) >= auth.ConsoleIdleLimit
	if idle || now.Sub(session.StartedAt) >= auth.ConsoleTotalLimit {
		if err := s.consoleSessions.Delete(ctx, session.IDHash); err != nil {
			s.logger.Warn("could not remove an expired console session", "error", err)
		}
		return nil, errNoConsoleSession
	}
	return session, nil
}

// authenticateConsole runs a cookie-authenticated request under its session, refusing a change
// that does not carry the session's forged-request value.
func (s *Server) authenticateConsole(next http.Handler, w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(consoleCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "missing authorization")
		return
	}
	session, err := s.liveConsoleSession(r.Context(), cookie.Value, true)
	if err != nil {
		if !errors.Is(err, errNoConsoleSession) {
			s.logHTTPIssue(slog.LevelError, "console sign-in lookup failed", r, err)
		}
		http.SetCookie(w, consoleCookie("", -1, reachedOverTLS(r)))
		writeError(w, http.StatusUnauthorized, "session expired")
		return
	}
	if changesSomething(r.Method) {
		sent := r.Header.Get(csrfHeader)
		if subtle.ConstantTimeCompare([]byte(sent), []byte(session.CSRFToken)) != 1 {
			writeError(w, http.StatusForbidden, "missing or wrong "+csrfHeader)
			return
		}
	}
	if now := s.now(); now.Sub(session.LastSeenAt) >= consoleTouchEvery {
		if err := s.consoleSessions.Touch(r.Context(), session.IDHash, now); err != nil {
			s.logHTTPIssue(slog.LevelWarn, "could not renew a console sign-in", r, err)
		}
	}

	claims := &auth.Claims{UserID: session.UserID, TenantID: session.TenantID, Email: session.Email,
		IsAdmin: session.IsAdmin}
	ctx := context.WithValue(r.Context(), claimsKey, claims)
	ctx = context.WithValue(ctx, consoleSessionKey{}, session)
	ctx = dbtx.WithTenant(ctx, session.TenantID, session.IsAdmin)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// changesSomething reports whether a request method can change state.
func changesSomething(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// consoleSessionKeyFor returns the rate-limit key of a request carrying a genuine session cookie.
func (s *Server) consoleSessionKeyFor(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(consoleCookieName)
	if err != nil {
		return "", false
	}
	id, ok := s.openSessionCookie(cookie.Value)
	if !ok {
		return "", false
	}
	sum := sha256.Sum256(id)
	return "session:" + base64.RawURLEncoding.EncodeToString(sum[:12]), true
}
