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
)

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

// denyIfNotAdmin returns the forbidden response and true when the caller lacks admin access.
func denyIfNotAdmin[T any](ctx context.Context, forbidden T) (T, bool) {
	if !isAdmin(ctx) {
		return forbidden, true
	}
	var zero T
	return zero, false
}

// isGroupOwner returns true if the authenticated user owns the given group or is an admin.
// Ungrouped devices (uuid.Nil) are accessible to all authenticated users.
func (s *Server) isGroupOwner(ctx context.Context, groupID uuid.UUID) bool {
	if isAdmin(ctx) {
		return true
	}
	if groupID == uuid.Nil {
		return true
	}
	group, err := s.store.GetGroup(ctx, groupID)
	if err != nil {
		return false
	}
	return group.OwnerID == ContextUserID(ctx)
}

// maxRequestBodySize is the maximum allowed request body size (1 MB).
const maxRequestBodySize = 1 << 20

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
				"path", r.URL.Path,
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

