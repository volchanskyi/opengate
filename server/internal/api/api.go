// Package api implements the HTTP server, REST endpoints, WebSocket upgrades,
// auth middleware, and SPA serving.
package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/correlate"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

//go:generate oapi-codegen -config ../../oapi-codegen.yaml ../../api/openapi.yaml

// AgentGetter finds connected agents by device ID or lists all.
type AgentGetter interface {
	GetAgent(deviceID db.DeviceID) *agentapi.AgentConn
	ListConnectedAgents() []*agentapi.AgentConn
	DeregisterAgent(ctx context.Context, deviceID db.DeviceID)
}

// CertProvider gives access to the server CA certificate and agent CSR signing.
type CertProvider interface {
	CACertPEM() []byte
	SignAgentCSR(csrDER []byte) ([]byte, error)
}

// CorrelationRanker ranks anomalous metric dimensions for a device window on
// demand. Implemented by *correlate.Engine; nil when telemetry is not
// configured, in which case the correlate endpoint reports 503.
type CorrelationRanker interface {
	Correlate(ctx context.Context, orgID uuid.UUID, req correlate.Request) (correlate.Result, error)
}

// ServerConfig holds all dependencies for the API server.
type ServerConfig struct {
	Store                 *db.PostgresStore
	Audit                 audit.Repository
	AuditHandlers         *audit.Handlers
	DeviceUpdates         updater.DeviceUpdateRepository
	Enrollment            updater.EnrollmentTokenRepository
	SecurityGroups        auth.SecurityGroupRepository
	Devices               device.Repository
	Groups                device.GroupRepository
	Hardware              device.HardwareRepository
	DeviceLogs            device.LogsRepository
	WebPush               notifications.WebPushRepository
	NotificationsHandlers *notifications.Handlers
	AMTDevices            amt.Repository
	AMTHandlers           *amt.Handlers
	Sessions              session.Repository
	SessionUseCase        *usecase.SessionService
	Users                 auth.UserRepository
	JWT                   *auth.JWTConfig
	Agents                AgentGetter
	AMT                   amt.Operator
	Cert                  CertProvider
	Correlate             CorrelationRanker
	Relay                 *relay.Relay
	Signaling             *signaling.Tracker
	Notifier              notifications.Notifier
	Signing               *updater.SigningKeys
	Manifests             *updater.ManifestStore
	GitHubRepo            string // GitHub repo for manifest auto-sync (e.g. "owner/repo")
	BaseURL               string // public base URL for install script (e.g. "https://opengate.example.com")
	QuicHost              string // override hostname for QUIC address in enrollment (bypasses CDN proxy)
	Logger                *slog.Logger
	WebDir                string // directory containing SPA static assets (optional)
	MetricsRegistry       *prometheus.Registry
	Metrics               *appmetrics.Metrics
}

// Server is the HTTP API server.
type Server struct {
	store           *db.PostgresStore
	audit           audit.Repository
	auditHandlers   *audit.Handlers
	deviceUpdates   updater.DeviceUpdateRepository
	enrollment      updater.EnrollmentTokenRepository
	securityGroups  auth.SecurityGroupRepository
	devices         device.Repository
	groups          device.GroupRepository
	hardware        device.HardwareRepository
	deviceLogs      device.LogsRepository
	webPush         notifications.WebPushRepository
	notifHandlers   *notifications.Handlers
	amtDevices      amt.Repository
	amtHandlers     *amt.Handlers
	sessions        session.Repository
	sessionUC       *usecase.SessionService
	users           auth.UserRepository
	jwt             *auth.JWTConfig
	agents          AgentGetter
	amt             amt.Operator
	cert            CertProvider
	correlate       CorrelationRanker
	relay           *relay.Relay
	signaling       *signaling.Tracker
	notifier        notifications.Notifier
	signing         *updater.SigningKeys
	manifests       *updater.ManifestStore
	githubRepo      string
	baseURL         string
	quicHost        string
	router          chi.Router
	logger          *slog.Logger
	webDir          string
	metricsRegistry *prometheus.Registry
	metrics         *appmetrics.Metrics
	loginLimiter    *emailLimiter
}

// resolveAuditHandlers returns the per-domain Handlers from cfg, or
// wraps the legacy Audit Repository to satisfy the new transport
// boundary. The api package consumes audit operations through audit.Handlers;
// tests that still wire only `Audit:`
// stay green via this fallback. main.go and new test code should pass
// AuditHandlers explicitly.
func resolveAuditHandlers(cfg ServerConfig) *audit.Handlers {
	if cfg.AuditHandlers != nil {
		return cfg.AuditHandlers
	}
	if cfg.Audit != nil {
		return audit.NewHandlers(cfg.Audit)
	}
	return nil
}

// resolveAMTHandlers — same pattern as resolveAuditHandlers. The amt
// Handlers struct needs BOTH the Repository (List/Get) and the Operator
// (PowerAction); fall back via cfg.AMTDevices + cfg.AMT when AMTHandlers
// is nil so existing test ServerConfig literals stay green.
func resolveAMTHandlers(cfg ServerConfig) *amt.Handlers {
	if cfg.AMTHandlers != nil {
		return cfg.AMTHandlers
	}
	if cfg.AMTDevices != nil && cfg.AMT != nil {
		return amt.NewHandlers(cfg.AMTDevices, cfg.AMT)
	}
	return nil
}

// resolveNotificationsHandlers — same fallback shape; notifications.Handlers
// requires BOTH the WebPushRepository (subscribe/unsubscribe) and the
// Notifier (VAPID public key).
func resolveNotificationsHandlers(cfg ServerConfig) *notifications.Handlers {
	if cfg.NotificationsHandlers != nil {
		return cfg.NotificationsHandlers
	}
	if cfg.WebPush != nil && cfg.Notifier != nil {
		return notifications.NewHandlers(cfg.WebPush, cfg.Notifier)
	}
	return nil
}

// resolveSessionUseCase constructs SessionService when not explicitly
// provided. Falls back from cfg.Sessions + cfg.Notifier + cfg.Audit
// when those three dependencies are available, preserving the narrow
// ServerConfig used by unit tests.
// Returns nil when prerequisites are missing — handler delegation must
// check for that before calling.
func resolveSessionUseCase(cfg ServerConfig) *usecase.SessionService {
	if cfg.SessionUseCase != nil {
		return cfg.SessionUseCase
	}
	if cfg.Sessions != nil && cfg.Notifier != nil && cfg.Audit != nil {
		return usecase.NewSessionService(cfg.Sessions, cfg.Notifier, cfg.Audit)
	}
	return nil
}

// NewServer creates an API server with all routes registered.
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		store:           cfg.Store,
		audit:           cfg.Audit,
		auditHandlers:   resolveAuditHandlers(cfg),
		deviceUpdates:   cfg.DeviceUpdates,
		enrollment:      cfg.Enrollment,
		securityGroups:  cfg.SecurityGroups,
		devices:         cfg.Devices,
		groups:          cfg.Groups,
		hardware:        cfg.Hardware,
		deviceLogs:      cfg.DeviceLogs,
		webPush:         cfg.WebPush,
		notifHandlers:   resolveNotificationsHandlers(cfg),
		amtDevices:      cfg.AMTDevices,
		amtHandlers:     resolveAMTHandlers(cfg),
		sessions:        cfg.Sessions,
		sessionUC:       resolveSessionUseCase(cfg),
		users:           cfg.Users,
		jwt:             cfg.JWT,
		agents:          cfg.Agents,
		amt:             cfg.AMT,
		cert:            cfg.Cert,
		correlate:       cfg.Correlate,
		relay:           cfg.Relay,
		signaling:       cfg.Signaling,
		notifier:        cfg.Notifier,
		signing:         cfg.Signing,
		manifests:       cfg.Manifests,
		githubRepo:      cfg.GitHubRepo,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		quicHost:        cfg.QuicHost,
		router:          chi.NewRouter(),
		logger:          cfg.Logger,
		webDir:          cfg.WebDir,
		metricsRegistry: cfg.MetricsRegistry,
		metrics:         cfg.Metrics,
		loginLimiter:    newEmailLimiter(loginMaxFailures, loginFailureWindow),
	}
	s.routes()
	return s
}

// Per-email failed-login throttle: lock an account's login path after
// loginMaxFailures failures within loginFailureWindow, independent of source IP.
const (
	loginMaxFailures   = 10
	loginFailureWindow = 15 * time.Minute
)

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

	// Prometheus metrics endpoint (internal only — not exposed through the ingress)
	if s.metricsRegistry != nil {
		r.Handle("/metrics", promhttp.HandlerFor(s.metricsRegistry, promhttp.HandlerOpts{}))
	}

	// Liveness probe — reports only that the process is up. Deliberately
	// dependency-free: a Postgres or Redis blip must NOT restart the pod, which
	// is readiness' job (/api/v1/health, GetHealth).
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

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

	// SPA static file serving with index.html fallback. Uses os.OpenRoot (Go
	// 1.24+) which rejects any path that tries to escape s.webDir via "..",
	// absolute paths, or symlinks resolving outside the root — taint-safe per
	// CodeQL's go/path-injection detector. Lifetime of *os.Root matches the
	// server's process.
	//
	// Three outcomes for a path that's not /api/ or /ws/:
	//   1. webRoot.Open succeeds → serve the static file.
	//   2. webRoot.Open returns fs.ErrNotExist → SPA fallback (index.html) so
	//      client-side routing handles deep links like /devices/123.
	//   3. Any other error (traversal attempt, permission, symlink escape) →
	//      explicit 404, NOT a silent SPA fallback.
	if s.webDir != "" {
		webRoot, err := os.OpenRoot(s.webDir)
		if err != nil {
			s.logger.Warn("SPA serving disabled — failed to open webDir", "error", err, "dir", s.webDir)
		} else {
			fileServer := http.FileServer(http.Dir(s.webDir))
			r.NotFound(func(w http.ResponseWriter, r *http.Request) {
				path := r.URL.Path
				if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/ws/") {
					http.NotFound(w, r)
					return
				}
				relPath := strings.TrimPrefix(path, "/")
				if relPath != "" {
					f, err := webRoot.Open(relPath)
					switch {
					case err == nil:
						_ = f.Close()
						fileServer.ServeHTTP(w, r)
						return
					case errors.Is(err, fs.ErrNotExist) && !strings.Contains(relPath, ".."):
						// Legitimate miss inside webDir → SPA fallback below.
						// Note: os.Root.Open evaluates path components left-to-right
						// and returns ErrNotExist on the FIRST missing component,
						// before it would detect a downstream escape. The `..` check
						// here covers that case (e.g. "static/../../../etc/passwd"
						// returns ErrNotExist because "static" doesn't exist in the
						// root, not because of the escape) so we reject visibly
						// instead of silently SPA-falling-back.
					default:
						// Traversal / permission / symlink escape — reject visibly.
						http.NotFound(w, r)
						return
					}
				}
				// SPA client-side routing fallback.
				r.URL.Path = "/"
				fileServer.ServeHTTP(w, r)
			})
		}
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
func (s *Server) auditLog(ctx context.Context, userID db.UserID, action, target, details string) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	auditCtx := context.Background()
	if ok {
		auditCtx = dbtx.WithTenant(auditCtx, tenant.OrgID, tenant.IsAdmin)
	} else {
		auditCtx = dbtx.WithDefaultTenant(auditCtx, false)
	}
	go func() {
		ctx, cancel := context.WithTimeout(auditCtx, 5*time.Second)
		defer cancel()
		if err := s.audit.Write(ctx, &audit.Event{
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
