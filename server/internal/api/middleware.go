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

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// relayPathPrefix precedes a secret session token on the WebSocket relay route.
const relayPathPrefix = "/ws/relay/"

// tokenPathPrefixes lists route prefixes whose next path segment is a bearer credential.
// Request logs leave the process, so those segments are redacted before they are written.
var tokenPathPrefixes = []string{
	relayPathPrefix,
	"/api/v1/enroll/",
	"/api/v1/sessions/",
}

// redactLogPath redacts the token segment of a credential-bearing path; other paths pass through.
func redactLogPath(path string) string {
	for _, prefix := range tokenPathPrefixes {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		token := path[len(prefix):]
		if token == "" || strings.Contains(token, "/") {
			return path
		}
		return prefix + protocol.RedactToken(token)
	}
	return path
}

// RequestTimeout returns middleware that applies a server-side timeout to requests.
// http.TimeoutHandler hides http.Hijacker, so WebSocket routes sit outside it.
func RequestTimeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":"request timeout"}`)
	}
}

// AuthRateLimiter returns a MiddlewareFunc that rate-limits login and register requests.
func AuthRateLimiter(rps float64, burst int, trust *TrustedProxies) MiddlewareFunc {
	limiter := RateLimiter(rps, burst, trust)
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

// AuthMiddleware returns middleware that validates JWT Bearer tokens and stores the claims.
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
			ctx = dbtx.WithTenant(ctx, claims.TenantID, claims.IsAdmin)
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

func isAdmin(ctx context.Context) bool {
	claims := ContextClaims(ctx)
	return claims != nil && claims.IsAdmin
}

const msgAdminRequired = "admin access required"
const msgUpdateNotConfigured = "update system not configured"
const msgForbidden = "forbidden"
const msgSecurityGroupNotFound = "security group not found"
const msgDeviceNotFound = "device not found"

// msgPurgeNotConfigured answers every erasure endpoint when no purge orchestrator is set.
const msgPurgeNotConfigured = "purge not configured"
const msgSessionNotFound = "session not found"

func denyIfNotAdmin[T any](ctx context.Context, forbidden T) (T, bool) {
	if !isAdmin(ctx) {
		return forbidden, true
	}
	var zero T
	return zero, false
}

// requireDeviceInScope runs the tenant-scoped lookup that authorizes device endpoints.
// A device in another tenant resolves to [device.ErrDeviceNotFound].
func (s *Server) requireDeviceInScope(ctx context.Context, id device.DeviceID) error {
	_, err := s.devices.Get(ctx, id)
	return err
}

// requireAMTDeviceInScope keeps AMT power commands in their tenant; the CIRA map has none.
func (s *Server) requireAMTDeviceInScope(ctx context.Context, amtUUID uuid.UUID) error {
	_, err := s.devices.GetByAMTUUID(ctx, amtUUID)
	return err
}

// requireSessionInScope reads the session under the request tenant, so another tenant's
// token resolves to [session.ErrSessionNotFound].
func (s *Server) requireSessionInScope(ctx context.Context, token string) error {
	_, err := s.sessions.Get(ctx, token)
	return err
}

// maxRequestBodySize is the request body limit in bytes (1 MiB).
const maxRequestBodySize = 1 << 20

// defaultRequestTimeout applies when ServerConfig leaves RequestTimeout at zero.
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

// contentSecurityPolicy is byte-identical to `ingress.csp` in deploy/helm/opengate/values.yaml.
// 'unsafe-inline' on style-src covers runtime UI styles; connect-src admits the relay WebSocket.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; " +
	"font-src 'self'; connect-src 'self' wss:; frame-ancestors 'none'"

// permissionsPolicy denies the camera, microphone, location and payment APIs the UI never uses.
const permissionsPolicy = "camera=(), microphone=(), geolocation=(), payment=()"

// SecurityHeaders returns middleware that sets security headers on every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Permissions-Policy", permissionsPolicy)
		next.ServeHTTP(w, r)
	})
}

// correlationAttrs returns the request-ID log fields for r, or nil when no request ID is set.
func correlationAttrs(r *http.Request) []any {
	if id := middleware.GetReqID(r.Context()); id != "" {
		return []any{"request_id", id}
	}
	return nil
}

// RequestLogger returns middleware that logs method, redacted path, status, duration, request ID.
func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)
			attrs := append([]any{
				"method", r.Method,
				"path", redactLogPath(r.URL.Path),
				"status", ww.status,
				"duration", time.Since(start),
			}, correlationAttrs(r)...)
			logger.Info("request", attrs...)
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

// Hijack implements http.Hijacker so WebSocket upgrades pass through the logger.
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
