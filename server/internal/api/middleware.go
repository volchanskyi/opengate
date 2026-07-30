package api

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// relayPathPrefix is the URL prefix of the WebSocket relay route whose final
// path segment is a secret session token that must never be logged in full.
const relayPathPrefix = "/ws/relay/"

// redactLogPath redacts the secret token segment of a relay WebSocket path so
// request logs (shipped to Loki) never carry a full relay token. Non-relay
// paths are returned unchanged.
func redactLogPath(path string) string {
	if !strings.HasPrefix(path, relayPathPrefix) {
		return path
	}
	token := path[len(relayPathPrefix):]
	if token == "" || strings.Contains(token, "/") {
		return path
	}
	return relayPathPrefix + protocol.RedactToken(token)
}

// RequestTimeout returns middleware that applies a server-side timeout to requests.
// It wraps http.TimeoutHandler, which does NOT implement http.Hijacker —
// WebSocket routes must be registered outside this middleware.
func RequestTimeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
	}
}

// AuthRateLimiter returns an oapi-codegen MiddlewareFunc that applies a tighter
// rate limit to authentication endpoints (login/register).
func AuthRateLimiter(rps float64, burst int) MiddlewareFunc {
	limiter := RateLimiter(rps, burst)
	return func(next http.Handler) http.Handler {
		limited := limiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/api/v1/auth/") {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type contextKey int

const claimsKey contextKey = 1

// AuthMiddleware returns middleware that validates JWT Bearer tokens
// and injects claims into the request context.
func AuthMiddleware(jwtCfg *auth.JWTConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				writeError(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				writeError(w, http.StatusUnauthorized, "invalid authorization header")
				return
			}

			claims, err := jwtCfg.ValidateToken(parts[1])
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			ctx = dbtx.WithTenant(ctx, claims.OrgID, claims.IsAdmin)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ContextClaims extracts JWT claims from the request context.
func ContextClaims(ctx context.Context) *auth.Claims {
	claims, _ := ctx.Value(claimsKey).(*auth.Claims)
	return claims
}

// ContextUserID extracts the authenticated user's ID from the request context.
func ContextUserID(ctx context.Context) uuid.UUID {
	if claims := ContextClaims(ctx); claims != nil {
		return claims.UserID
	}
	return uuid.Nil
}

// isAdmin returns true if the request context contains admin claims.
func isAdmin(ctx context.Context) bool {
	claims := ContextClaims(ctx)
	return claims != nil && claims.IsAdmin
}

const msgAdminRequired = "admin access required"
const msgUpdateNotConfigured = "update system not configured"
const msgForbidden = "forbidden"
const msgSecurityGroupNotFound = "security group not found"
const msgDeviceNotFound = "device not found"
const msgSessionNotFound = "session not found"

// denyIfNotAdmin returns the forbidden response and true when the caller lacks admin access.
func denyIfNotAdmin[T any](ctx context.Context, forbidden T) (T, bool) {
	if !isAdmin(ctx) {
		return forbidden, true
	}
	var zero T
	return zero, false
}

// requireDeviceInScope asserts that a device exists inside the caller's
// organization. Organization is the visibility boundary, so this lookup is the
// authorization step for every device-addressed endpoint: the repository runs
// it under the request tenant, and a device in another organization resolves to
// [device.ErrDeviceNotFound] rather than to a forbidden response. Callers map
// that error onto their own typed 404.
func (s *Server) requireDeviceInScope(ctx context.Context, id device.DeviceID) error {
	_, err := s.devices.Get(ctx, id)
	return err
}

// requireAMTDeviceInScope asserts that an Intel AMT identity belongs to a
// managed device inside the caller's organization. The CIRA connection map that
// serves power commands is keyed by AMT UUID alone and carries no tenant, so
// this tenant-scoped lookup is what keeps a command inside its organization.
func (s *Server) requireAMTDeviceInScope(ctx context.Context, amtUUID uuid.UUID) error {
	_, err := s.devices.GetByAMTUUID(ctx, amtUUID)
	return err
}

// requireSessionInScope asserts that a session token names a session inside the
// caller's organization. Ending a session is a device command, so membership is
// the whole gate; the repository read is tenant-scoped, so a token from another
// organization resolves to [session.ErrSessionNotFound].
func (s *Server) requireSessionInScope(ctx context.Context, token string) error {
	_, err := s.sessions.Get(ctx, token)
	return err
}

// maxRequestBodySize is the maximum allowed request body size (1 MB).
const maxRequestBodySize = 1 << 20

// defaultRequestTimeout bounds a single API request when ServerConfig leaves
// RequestTimeout at zero.
const defaultRequestTimeout = 30 * time.Second

// MaxBodySize returns middleware that limits request body size.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders returns middleware that adds security headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

// RequestLogger returns middleware that logs each request with method, path, status, and duration.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", redactLogPath(r.URL.Path),
				"status", ww.status,
				"duration", time.Since(start),
			)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements http.Hijacker so WebSocket upgrades work through the logger middleware.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Debug("failed to write error response", "error", err)
	}
}
