// Package api implements the HTTP server, REST endpoints, WebSocket upgrades,
// auth middleware, and SPA serving.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
)

//go:generate oapi-codegen -config ../../oapi-codegen.yaml ../../api/openapi.yaml

// AgentGetter finds a connected agent by device ID.
type AgentGetter interface {
	GetAgent(deviceID db.DeviceID) *agentapi.AgentConn
}

// Server is the HTTP API server.
type Server struct {
	store     db.Store
	jwt       *auth.JWTConfig
	agents    AgentGetter
	relay     *relay.Relay
	signaling *signaling.Tracker
	notifier  notifications.Notifier
	router    chi.Router
	logger    *slog.Logger
}

// NewServer creates an API server with all routes registered.
func NewServer(store db.Store, jwtCfg *auth.JWTConfig, agents AgentGetter, relay *relay.Relay, sigTracker *signaling.Tracker, notifier notifications.Notifier, logger *slog.Logger) *Server {
	s := &Server{
		store:     store,
		jwt:       jwtCfg,
		agents:    agents,
		relay:     relay,
		signaling: sigTracker,
		notifier:  notifier,
		router:    chi.NewRouter(),
		logger:    logger,
	}
	s.routes()
	return s
}

// ServeHTTP implements the http.Handler interface.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) routes() {
	r := s.router

	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(RequestLogger(s.logger))

	strictHandler := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{requestContextMiddleware}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusInternalServerError, "internal error")
		},
	})

	HandlerWithOptions(strictHandler, ChiServerOptions{
		BaseRouter: r,
		Middlewares: []MiddlewareFunc{
			s.oapiAuthMiddleware(),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			writeError(w, http.StatusBadRequest, err.Error())
		},
	})

	// WebSocket relay — token in URL acts as auth
	r.Get("/ws/relay/{token}", s.handleRelayWebSocket)
}

// oapiAuthMiddleware returns a middleware that applies JWT validation
// only to endpoints that declare security in the OpenAPI spec.
func (s *Server) oapiAuthMiddleware() MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(BearerAuthScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			AuthMiddleware(s.jwt)(next).ServeHTTP(w, r)
		})
	}
}

// auditLog writes an audit event in a fire-and-forget goroutine.
func (s *Server) auditLog(userID db.UserID, action, target, details string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.WriteAuditEvent(ctx, &db.AuditEvent{
			UserID:    userID,
			Action:    action,
			Target:    target,
			Details:   details,
			CreatedAt: time.Now(),
		}); err != nil {
			s.logger.Warn("audit log write failed", "action", action, "error", err)
		}
	}()
}

type httpRequestKey struct{}

// requestContextMiddleware injects the HTTP request into the strict handler context
// so handlers can access host/scheme info.
func requestContextMiddleware(f StrictHandlerFunc, _ string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		ctx = context.WithValue(ctx, httpRequestKey{}, r)
		return f(ctx, w, r, request)
	}
}

// httpRequestFromContext retrieves the HTTP request from context.
func httpRequestFromContext(ctx context.Context) *http.Request {
	r, _ := ctx.Value(httpRequestKey{}).(*http.Request)
	return r
}
