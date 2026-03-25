// Package api implements the HTTP server, REST endpoints, WebSocket upgrades,
// auth middleware, and SPA serving.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/db"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/mps/wsman"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

//go:generate oapi-codegen -config ../../oapi-codegen.yaml ../../api/openapi.yaml

// AgentGetter finds connected agents by device ID or lists all.
type AgentGetter interface {
	GetAgent(deviceID db.DeviceID) *agentapi.AgentConn
	ListConnectedAgents() []*agentapi.AgentConn
	DeregisterAgent(ctx context.Context, deviceID db.DeviceID)
}

// AMTOperator provides high-level AMT device operations.
type AMTOperator interface {
	PowerAction(ctx context.Context, amtUUID uuid.UUID, state int) error
	QueryDeviceInfo(ctx context.Context, amtUUID uuid.UUID) (*wsman.DeviceInfo, error)
	ConnectedDeviceCount() int
}

// CertProvider gives access to the server CA certificate and agent CSR signing.
type CertProvider interface {
	CACertPEM() []byte
	SignAgentCSR(csrDER []byte) ([]byte, error)
}

// ServerConfig holds all dependencies for the API server.
type ServerConfig struct {
	Store     db.Store
	JWT       *auth.JWTConfig
	Agents    AgentGetter
	AMT       AMTOperator
	Cert      CertProvider
	Relay     *relay.Relay
	Signaling *signaling.Tracker
	Notifier  notifications.Notifier
	Signing   *updater.SigningKeys
	Manifests  *updater.ManifestStore
	GitHubRepo string // GitHub repo for manifest auto-sync (e.g. "owner/repo")
	BaseURL    string // public base URL for install script (e.g. "https://opengate.example.com")
	QuicHost   string // override hostname for QUIC address in enrollment (bypasses CDN proxy)
	Logger          *slog.Logger
	WebDir          string // directory containing SPA static assets (optional)
	MetricsRegistry *prometheus.Registry
	Metrics         *appmetrics.Metrics
}

// Server is the HTTP API server.
type Server struct {
	store     db.Store
	jwt       *auth.JWTConfig
	agents    AgentGetter
	amt       AMTOperator
	cert      CertProvider
	relay     *relay.Relay
	signaling *signaling.Tracker
	notifier  notifications.Notifier
	signing   *updater.SigningKeys
	manifests  *updater.ManifestStore
	githubRepo string
	baseURL    string
	quicHost   string
	router     chi.Router
	logger     *slog.Logger
	webDir     string
	metricsRegistry *prometheus.Registry
	metrics         *appmetrics.Metrics
}

// NewServer creates an API server with all routes registered.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		store:     cfg.Store,
		jwt:       cfg.JWT,
		agents:    cfg.Agents,
		amt:       cfg.AMT,
		cert:      cfg.Cert,
		relay:     cfg.Relay,
		signaling: cfg.Signaling,
		notifier:  cfg.Notifier,
		signing:   cfg.Signing,
		manifests:  cfg.Manifests,
		githubRepo: cfg.GitHubRepo,
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		quicHost:   cfg.QuicHost,
		router:          chi.NewRouter(),
		logger:          cfg.Logger,
		webDir:          cfg.WebDir,
		metricsRegistry: cfg.MetricsRegistry,
		metrics:         cfg.Metrics,
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
	if s.metrics != nil {
		r.Use(appmetrics.HTTPMiddleware(s.metrics))
	}
	r.Use(SecurityHeaders)
	r.Use(MaxBodySize(maxRequestBodySize))
	r.Use(RequestLogger(s.logger))

	// Prometheus metrics endpoint (not proxied by Caddy — internal only)
	if s.metricsRegistry != nil {
		r.Handle("/metrics", promhttp.HandlerFor(s.metricsRegistry, promhttp.HandlerOpts{}))
	}

	strictHandler := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{requestContextMiddleware}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logger.Warn("request validation error", "error", err, "path", r.URL.Path)
			writeError(w, http.StatusBadRequest, "invalid request")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logger.Error("response error", "error", err, "path", r.URL.Path)
			writeError(w, http.StatusInternalServerError, "internal error")
		},
	})

	// API routes in a group with rate limiting and request timeout.
	// WebSocket routes stay outside so TimeoutHandler doesn't break upgrades.
	r.Group(func(apiRouter chi.Router) {
		apiRouter.Use(RequestTimeout(30 * time.Second))
		apiRouter.Use(RateLimiter(100, 200))

		HandlerWithOptions(strictHandler, ChiServerOptions{
			BaseRouter: apiRouter,
			Middlewares: []MiddlewareFunc{
				s.oapiAuthMiddleware(),
				AuthRateLimiter(10, 20),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				s.logger.Warn("request error", "error", err, "path", r.URL.Path)
				writeError(w, http.StatusBadRequest, "invalid request")
			},
		})
	})

	// WebSocket relay — token in URL acts as auth (no timeout middleware)
	r.Get("/ws/relay/{token}", s.handleRelayWebSocket)

	// SPA static file serving with index.html fallback
	if s.webDir != "" {
		webFS := http.Dir(s.webDir)
		fileServer := http.FileServer(webFS)
		r.NotFound(func(w http.ResponseWriter, r *http.Request) {
			// Try to serve the exact file first (JS, CSS, images, etc.)
			path := r.URL.Path
			if !strings.HasPrefix(path, "/api/") && !strings.HasPrefix(path, "/ws/") {
				// Sanitize path to prevent directory traversal attacks.
				cleanPath := filepath.Clean(filepath.Join(s.webDir, path))
				if !strings.HasPrefix(cleanPath, filepath.Clean(s.webDir)+string(filepath.Separator)) &&
					cleanPath != filepath.Clean(s.webDir) {
					http.NotFound(w, r)
					return
				}
				if f, err := os.Open(cleanPath); err == nil {
					f.Close()
					fileServer.ServeHTTP(w, r)
					return
				}
				// Fall back to index.html for SPA client-side routing
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
		})
	}
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
			s.logger.Error("audit log write failed", "action", action, "error", err)
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
