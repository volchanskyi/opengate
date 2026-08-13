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
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/organization"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

//go:generate oapi-codegen -config ../../oapi-codegen.yaml ../../api/openapi.yaml

// AgentControl is the api package's port over a connected agent. It is exactly
// the surface the HTTP handlers use: the four control-writes, the two
// synchronous request/response reads, and a metadata snapshot. Depending on this
// consumer-defined interface instead of the concrete *agentapi.AgentConn lets a
// test harness substitute a fault-decorating implementation for the
// agent.control-write scenario without compiling fault code into the server.
type AgentControl interface {
	// Control-writes (server → agent). The capability-gated sends return a typed
	// capability error the handlers detect via agentapi.IsCapabilityError.
	SendSessionRequest(ctx context.Context, token protocol.SessionToken, relayURL string, perms protocol.Permissions) error
	SendAgentUpdate(ctx context.Context, version, url, sha256, signature string) error
	SendRestartAgent(ctx context.Context, reason string) error
	SendRequestHardwareReport(ctx context.Context) error
	SendSetMaintenanceMode(ctx context.Context, enabled bool) error

	// Synchronous request/response reads (server → agent → server): each sends a
	// request and blocks for the agent's bounded response.
	RequestLogsSync(ctx context.Context, filter device.LogFilter) ([]device.LogEntry, int, []string, error)
	RequestLocalHistorySync(ctx context.Context, dim string, fromTS, toTS int64, maxPoints uint32) ([]protocol.HistoryPoint, bool, error)

	// Meta returns a consistent snapshot of the agent's registration metadata.
	Meta() agentapi.AgentMeta
}

// AgentGetter finds connected agents by device ID or lists all.
type AgentGetter interface {
	GetAgent(deviceID db.DeviceID) AgentControl
	ListConnectedAgents() []AgentControl
	DeregisterAgent(ctx context.Context, deviceID db.DeviceID)
}

// CertProvider gives access to the server CA certificate and agent CSR signing.
type CertProvider interface {
	CACertPEM() []byte
	SignAgentCSR(csrDER []byte) ([]byte, error)
}

// MetricsReader reads tenant-scoped numeric telemetry for chart windows and the
// fleet health badge. Implemented by *telemetry.VMClient; nil when telemetry is
// not configured, in which case the metrics endpoint reports 503 and the device
// list omits anomaly_rate.
type MetricsReader interface {
	QueryRange(ctx context.Context, tenantID uuid.UUID, rq telemetry.RangeQuery) ([]telemetry.RangeSeries, error)
	QueryInstant(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time) ([]telemetry.InstantValue, error)
	QueryInstantLookback(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time, lookback time.Duration) ([]telemetry.InstantValue, error)
	// CountAnomalyBands returns how many devices fall in each edge-health band,
	// counted inside the time-series store so the dashboard rollup stays O(1) in
	// fleet size.
	CountAnomalyBands(ctx context.Context, tenantID uuid.UUID, watch, anomalous float64, at time.Time, lookback time.Duration) (telemetry.BandCounts, error)
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
	Sites                 device.SiteRepository
	Organizations         organization.Repository
	Hardware              device.HardwareRepository
	Inventory             inventory.Repository
	WebPush               notifications.WebPushRepository
	NotificationsHandlers *notifications.Handlers
	AMTHandlers           *amt.Handlers
	Sessions              session.Repository
	SessionUseCase        *usecase.SessionService
	Users                 auth.UserRepository
	JWT                   *auth.JWTConfig
	Agents                AgentGetter
	AMT                   amt.Operator
	Cert                  CertProvider
	TelemetryReader       MetricsReader
	Purger                DevicePurger
	PurgeJobs             PurgeJobReader
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
	// RequestTimeout bounds a single API request. Zero selects
	// defaultRequestTimeout. Tests inject a short budget so timeout-boundary
	// behavior is provable in milliseconds rather than in wall-clock seconds.
	RequestTimeout time.Duration
	// RelayPeerTimeout bounds how long one relay side may wait for its peer.
	// Zero selects defaultRelayPeerTimeout; paired sessions are not time-limited.
	RelayPeerTimeout time.Duration
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
	sites           device.SiteRepository
	organizations   organization.Repository
	hardware        device.HardwareRepository
	inventory       inventory.Repository
	webPush         notifications.WebPushRepository
	notifHandlers   *notifications.Handlers
	amtHandlers     *amt.Handlers
	sessions        session.Repository
	sessionUC       *usecase.SessionService
	users           auth.UserRepository
	jwt             *auth.JWTConfig
	agents          AgentGetter
	amt             amt.Operator
	cert            CertProvider
	telemetryReader MetricsReader
	purger          DevicePurger
	purgeJobs       PurgeJobReader
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
	requestTimeout  time.Duration
	peerWaitTimeout time.Duration
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

// resolveAMTHandlers — same pattern as resolveAuditHandlers. The amt Handlers
// struct needs the Operator (PowerAction); fall back via cfg.AMT when
// AMTHandlers is nil so existing test ServerConfig literals stay green.
func resolveAMTHandlers(cfg ServerConfig) *amt.Handlers {
	if cfg.AMTHandlers != nil {
		return cfg.AMTHandlers
	}
	if cfg.AMT != nil {
		return amt.NewHandlers(cfg.AMT)
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
		sites:           cfg.Sites,
		organizations:   cfg.Organizations,
		hardware:        cfg.Hardware,
		inventory:       cfg.Inventory,
		webPush:         cfg.WebPush,
		notifHandlers:   resolveNotificationsHandlers(cfg),
		amtHandlers:     resolveAMTHandlers(cfg),
		sessions:        cfg.Sessions,
		sessionUC:       resolveSessionUseCase(cfg),
		users:           cfg.Users,
		jwt:             cfg.JWT,
		agents:          cfg.Agents,
		amt:             cfg.AMT,
		cert:            cfg.Cert,
		telemetryReader: cfg.TelemetryReader,
		purger:          cfg.Purger,
		purgeJobs:       cfg.PurgeJobs,
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
		requestTimeout:  cfg.RequestTimeout,
		peerWaitTimeout: cfg.RelayPeerTimeout,
	}
	if s.requestTimeout <= 0 {
		s.requestTimeout = defaultRequestTimeout
	}
	if s.peerWaitTimeout <= 0 {
		s.peerWaitTimeout = defaultRelayPeerTimeout
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
			s.logHTTPIssue(slog.LevelWarn, "request validation error", r, err)
			writeError(w, http.StatusBadRequest, "invalid request")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			s.logHTTPIssue(slog.LevelError, "response error", r, err)
			writeError(w, http.StatusInternalServerError, "internal error")
		},
	})

	// API routes in a subrouter with rate limiting and request timeout.
	// WebSocket routes stay outside so TimeoutHandler doesn't break upgrades.
	r.Group(func(apiRouter chi.Router) {
		apiRouter.Use(RequestTimeout(s.requestTimeout))
		apiRouter.Use(RateLimiter(100, 200))

		HandlerWithOptions(strictHandler, ChiServerOptions{
			BaseRouter: apiRouter,
			Middlewares: []MiddlewareFunc{
				s.oapiAuthMiddleware(),
				AuthRateLimiter(10, 20),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				s.logHTTPIssue(slog.LevelWarn, "request error", r, err)
				writeError(w, http.StatusBadRequest, "invalid request")
			},
		})
	})

	// WebSocket relay — token in URL acts as auth (no timeout middleware)
	r.Get("/ws/relay/{token}", s.handleRelayWebSocket)

	s.registerSPA(r)
}

// logHTTPIssue logs a request-scoped problem with the path redacted and the
// request's correlation ID attached, so the entry can be tied to the access-log
// line for the same request in Loki.
func (s *Server) logHTTPIssue(level slog.Level, msg string, r *http.Request, err error) {
	s.logger.Log(r.Context(), level, msg, append([]any{
		"error", err,
		"path", redactLogPath(r.URL.Path),
	}, correlationAttrs(r)...)...)
}

// registerSPA installs static file serving with an index.html fallback. It uses
// os.OpenRoot, which rejects any path that tries to escape s.webDir via "..",
// absolute paths, or symlinks resolving outside the root — taint-safe per
// CodeQL's go/path-injection detector. Lifetime of *os.Root matches the
// server's process.
func (s *Server) registerSPA(r chi.Router) {
	if s.webDir == "" {
		return
	}
	webRoot, err := os.OpenRoot(s.webDir)
	if err != nil {
		s.logger.Warn("SPA serving disabled — failed to open webDir", "error", err, "dir", s.webDir)
		return
	}
	fileServer := http.FileServer(http.Dir(s.webDir))
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws/") {
			http.NotFound(w, r)
			return
		}
		if served, ok := serveStaticFile(w, r, webRoot, fileServer); ok {
			if !served {
				http.NotFound(w, r)
			}
			return
		}
		// SPA client-side routing fallback.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

// serveStaticFile resolves the request path inside webRoot. It reports handled
// = false when the caller should fall back to the SPA index; when handled is
// true, served says whether the file was written or the request must be
// rejected outright.
//
// Three outcomes for a path that's not /api/ or /ws/:
//  1. webRoot.Open succeeds → serve the static file.
//  2. webRoot.Open returns fs.ErrNotExist → SPA fallback so client-side routing
//     handles deep links like /devices/123.
//  3. Any other error (traversal attempt, permission, symlink escape) →
//     explicit 404, NOT a silent SPA fallback.
func serveStaticFile(w http.ResponseWriter, r *http.Request, webRoot *os.Root, fileServer http.Handler) (served, handled bool) {
	relPath := strings.TrimPrefix(r.URL.Path, "/")
	if relPath == "" {
		return false, false
	}
	f, err := webRoot.Open(relPath)
	switch {
	case err == nil:
		_ = f.Close()
		fileServer.ServeHTTP(w, r)
		return true, true
	case errors.Is(err, fs.ErrNotExist) && !strings.Contains(relPath, ".."):
		// Legitimate miss inside webDir → SPA fallback.
		//
		// os.Root.Open evaluates path components left-to-right and returns
		// ErrNotExist on the FIRST missing component, before it would detect a
		// downstream escape. The ".." check covers that case (e.g.
		// "static/../../../etc/passwd" returns ErrNotExist because "static"
		// doesn't exist in the root, not because of the escape) so such a path
		// is rejected visibly instead of silently SPA-falling-back.
		return false, false
	default:
		// Traversal / permission / symlink escape — reject visibly.
		return false, true
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
	auditCtx := context.WithoutCancel(ctx)
	if ok {
		auditCtx = dbtx.WithTenant(auditCtx, tenant.TenantID, tenant.IsAdmin)
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
