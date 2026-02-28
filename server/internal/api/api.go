// Package api implements the HTTP server, REST endpoints, WebSocket upgrades,
// auth middleware, and SPA serving.
package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// Server is the HTTP API server.
type Server struct {
	store  db.Store
	jwt    *auth.JWTConfig
	router chi.Router
	logger *slog.Logger
}

// NewServer creates an API server with all routes registered.
func NewServer(store db.Store, jwtCfg *auth.JWTConfig, logger *slog.Logger) *Server {
	s := &Server{
		store:  store,
		jwt:    jwtCfg,
		router: chi.NewRouter(),
		logger: logger,
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

	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Get("/health", s.handleHealth)
		r.Post("/auth/register", s.handleRegister)
		r.Post("/auth/login", s.handleLogin)

		// Protected
		r.Group(func(r chi.Router) {
			r.Use(AuthMiddleware(s.jwt))

			r.Get("/devices", s.handleListDevices)
			r.Get("/devices/{id}", s.handleGetDevice)
			r.Delete("/devices/{id}", s.handleDeleteDevice)

			r.Post("/groups", s.handleCreateGroup)
			r.Get("/groups", s.handleListGroups)
			r.Get("/groups/{id}", s.handleGetGroup)
			r.Delete("/groups/{id}", s.handleDeleteGroup)

			r.Get("/users", s.handleListUsers)
			r.Get("/users/me", s.handleGetMe)
			r.Delete("/users/{id}", s.handleDeleteUser)
		})
	})
}
