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
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
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
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
	"github.com/volchanskyi/opengate/server/internal/usecase"
)

//go:generate oapi-codegen -config ../../oapi-codegen.yaml ../../api/openapi.yaml

// AgentControl is the api package's port over a connected agent: control-writes,
// synchronous reads and a metadata snapshot. A test harness can wrap it to inject faults.
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

// AgentGetter finds connected agents by device ID or lists all, and carries an
// administrator's rule change out to the machines already holding the old one.
type AgentGetter interface {
	GetAgent(deviceID db.DeviceID) AgentControl
	ListConnectedAgents() []AgentControl
	// RefreshAlertRules re-resolves and delivers the ruleset to every connected machine
	// of one customer and returns how many were reached.
	RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int
	// RefreshAlertRulesForTenant does the same for every customer in one
	// tenant, which is the reach a tenant-wide stop asks for.
	RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int
}

// CertProvider gives access to the server CA certificate and agent CSR signing.
type CertProvider interface {
	CACertPEM() []byte
	SignAgentCSR(csrDER []byte) ([]byte, error)
}

// MetricsReader reads tenant-scoped numeric telemetry for chart windows and the fleet
// health badge. Nil when telemetry is unconfigured: metrics answer 503, anomaly_rate is omitted.
type MetricsReader interface {
	QueryRange(ctx context.Context, tenantID uuid.UUID, rq telemetry.RangeQuery) ([]telemetry.RangeSeries, error)
	QueryInstant(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time) ([]telemetry.InstantValue, error)
	QueryInstantLookback(ctx context.Context, tenantID uuid.UUID, metric string, matchers map[string]string, at time.Time, lookback time.Duration) ([]telemetry.InstantValue, error)
	// CountAnomalyBands returns how many devices fall in each edge-health band, counted in the
	// time-series store so the rollup is O(1) in fleet size.
	CountAnomalyBands(ctx context.Context, tenantID uuid.UUID, watch, anomalous float64, at time.Time, lookback time.Duration) (telemetry.BandCounts, error)
}

// IncidentStore is the api package's port over the investigation store: the triage
// queue, one room with its contents, and the moves a person makes on a room.
type IncidentStore interface {
	Queue(ctx context.Context, filter alerts.Filter) (alerts.Page, error)
	Incident(ctx context.Context, incidentID, organizationID uuid.UUID) (alerts.Incident, error)
	Investigation(ctx context.Context, incidentID, organizationID uuid.UUID) (alerts.Investigation, error)
	Transition(ctx context.Context, incidentID uuid.UUID, change alerts.Change) error
	Assign(ctx context.Context, incidentID, assignee, actor uuid.UUID) error
	Comment(ctx context.Context, incidentID, actor uuid.UUID, note string) (alerts.Event, error)
	Evidence(ctx context.Context, incidentID, alertID uuid.UUID) ([]byte, string, error)
}

// RulePack is the curated rule catalogue compiled into this build. Its definitions are
// read-only and validated and cost-bounded in CI.
type RulePack interface {
	All() []rules.Definition
	// Lookup resolves one rule id to its definition.
	Lookup(id string) (rules.Definition, bool)
}

// RuleRolloutReader reads how far each rule has reached across one customer's
// estate.
type RuleRolloutReader interface {
	ListRollouts(ctx context.Context, organizationID uuid.UUID) (map[string]rules.Rollout, error)
}

// RuleAdmin is everything an operator may change about a rule, plus the labels a
// rule can target.
type RuleAdmin interface {
	ListBindings(ctx context.Context, organizationID uuid.UUID) ([]rules.Binding, error)
	UpsertBinding(ctx context.Context, pack rules.Pack, b rules.Binding) error
	DeleteBinding(ctx context.Context, id uuid.UUID) error

	UpsertRollout(ctx context.Context, r rules.Rollout) error
	StopRule(ctx context.Context, organizationID uuid.UUID, ruleID, updatedBy string) error
	ResumeRule(ctx context.Context, organizationID uuid.UUID, ruleID, updatedBy string) error
	StopRuleTenantWide(ctx context.Context, ruleID, updatedBy string) error
	ResumeRuleTenantWide(ctx context.Context, ruleID, updatedBy string) error

	ReconcileClamps(ctx context.Context, pack rules.Pack, organizationID uuid.UUID) ([]rules.Clamp, error)
	AcknowledgeClamp(ctx context.Context, id uuid.UUID, acknowledgedBy string) error

	ListLabels(ctx context.Context, organizationID uuid.UUID) ([]rules.Label, error)
	CreateLabel(ctx context.Context, l rules.Label) error
	Label(ctx context.Context, id uuid.UUID) (rules.Label, error)
	DeleteLabel(ctx context.Context, id uuid.UUID) error
	AssignTag(ctx context.Context, deviceID, labelID uuid.UUID, assignedBy string) error
	ClearTag(ctx context.Context, deviceID uuid.UUID, key string) error
	TagsFor(ctx context.Context, deviceID uuid.UUID) (map[string]string, error)
	ListTagAssignments(ctx context.Context, organizationID uuid.UUID) (map[uuid.UUID]map[string]string, error)
}

// AlertBudget is a customer's alert ceilings and how many alerts each rule has let through.
type AlertBudget interface {
	Limits(ctx context.Context, organizationID uuid.UUID) (alerts.Limits, error)
	UpsertLimits(ctx context.Context, l alerts.Limits) error
	RuleNoise(ctx context.Context, organizationID uuid.UUID) (map[string]alerts.Noise, error)
}

// RuleCoverageReader reads how much of one customer's estate each rule watches, against a
// fleet size the caller passes in so the split and its total come from one moment.
type RuleCoverageReader interface {
	RuleCoverage(ctx context.Context, organizationID uuid.UUID, fleetSize int) map[string]agentapi.RuleCoverageCounts
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
	Investigations        IncidentStore
	RuleCatalogue         RulePack
	RuleRollouts          RuleRolloutReader
	RuleCoverage          RuleCoverageReader
	RuleAdmin             RuleAdmin
	AlertBudget           AlertBudget
	Relay                 *relay.Relay
	Signaling             signaling.Config
	Notifier              notifications.Notifier
	Signing               *updater.SigningKeys
	Manifests             *updater.ManifestStore
	GitHubRepo            string // GitHub repo for manifest auto-sync (e.g. "owner/repo")
	BaseURL               string // public base URL for install script (e.g. "https://opengate.example.com")
	QuicHost              string // override hostname for QUIC address in enrollment (bypasses CDN proxy)
	// TrustedProxies names the reverse proxies whose X-Forwarded-For selects the caller's
	// rate-limit allowance. Nil trusts none, as for a server reached directly.
	TrustedProxies *TrustedProxies
	Logger         *slog.Logger
	WebDir         string // directory containing SPA static assets (optional)
	Metrics        *appmetrics.Metrics
	// RequestTimeout bounds a single API request. Zero selects defaultRequestTimeout.
	RequestTimeout time.Duration
	// RelayPeerTimeout bounds how long one relay side may wait for its peer.
	// Zero selects defaultRelayPeerTimeout; paired sessions are not time-limited.
	RelayPeerTimeout time.Duration
	// RelayPingInterval is how often a relay side pings its peer, and how long the answer
	// may take. Zero selects defaultRelayPingInterval.
	RelayPingInterval time.Duration
	// Lifetime is closed on process shutdown. websocket.Accept hijacks the connection, so only
	// this channel reaches a parked relay handler; a nil channel waits for the session to end.
	Lifetime <-chan struct{}
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
	investigations  IncidentStore
	ruleCatalogue   RulePack
	ruleRollouts    RuleRolloutReader
	ruleCoverage    RuleCoverageReader
	ruleAdmin       RuleAdmin
	alertBudget     AlertBudget
	relay           *relay.Relay
	signaling       signaling.Config
	notifier        notifications.Notifier
	signing         *updater.SigningKeys
	manifests       *updater.ManifestStore
	githubRepo      string
	baseURL         string
	quicHost        string
	trustedProxies  *TrustedProxies
	router          chi.Router
	logger          *slog.Logger
	webDir          string
	metrics         *appmetrics.Metrics
	loginLimiter    *emailLimiter
	// auditSlots bounds concurrent audit writes; auditSlotsOnce creates it on first use.
	// A Server built outside NewServer would otherwise hold a nil channel that sheds every write.
	auditSlots      chan struct{}
	auditSlotsOnce  sync.Once
	requestTimeout  time.Duration
	peerWaitTimeout time.Duration
	pingInterval    time.Duration
	lifetime        <-chan struct{}
}

// resolveAuditHandlers returns cfg.AuditHandlers, or wraps cfg.Audit when only that is set.
func resolveAuditHandlers(cfg ServerConfig) *audit.Handlers {
	if cfg.AuditHandlers != nil {
		return cfg.AuditHandlers
	}
	if cfg.Audit != nil {
		return audit.NewHandlers(cfg.Audit)
	}
	return nil
}

// resolveAMTHandlers returns cfg.AMTHandlers, or builds them from cfg.AMT when unset.
func resolveAMTHandlers(cfg ServerConfig) *amt.Handlers {
	if cfg.AMTHandlers != nil {
		return cfg.AMTHandlers
	}
	if cfg.AMT != nil {
		return amt.NewHandlers(cfg.AMT)
	}
	return nil
}

// resolveNotificationsHandlers returns cfg.NotificationsHandlers, or builds them when both
// the web-push repository and the notifier are set.
func resolveNotificationsHandlers(cfg ServerConfig) *notifications.Handlers {
	if cfg.NotificationsHandlers != nil {
		return cfg.NotificationsHandlers
	}
	if cfg.WebPush != nil && cfg.Notifier != nil {
		return notifications.NewHandlers(cfg.WebPush, cfg.Notifier)
	}
	return nil
}

// resolveSessionUseCase returns cfg.SessionUseCase, or builds it from the sessions, notifier
// and audit dependencies; it returns nil when any is missing.
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
		investigations:  cfg.Investigations,
		ruleCatalogue:   cfg.RuleCatalogue,
		ruleRollouts:    cfg.RuleRollouts,
		ruleCoverage:    cfg.RuleCoverage,
		ruleAdmin:       cfg.RuleAdmin,
		alertBudget:     cfg.AlertBudget,
		relay:           cfg.Relay,
		signaling:       cfg.Signaling,
		notifier:        cfg.Notifier,
		signing:         cfg.Signing,
		manifests:       cfg.Manifests,
		githubRepo:      cfg.GitHubRepo,
		baseURL:         strings.TrimRight(cfg.BaseURL, "/"),
		quicHost:        cfg.QuicHost,
		trustedProxies:  cfg.TrustedProxies,
		router:          chi.NewRouter(),
		logger:          cfg.Logger,
		webDir:          cfg.WebDir,
		metrics:         cfg.Metrics,
		loginLimiter:    newEmailLimiter(loginMaxFailures, loginFailureWindow),
		requestTimeout:  cfg.RequestTimeout,
		peerWaitTimeout: cfg.RelayPeerTimeout,
		pingInterval:    cfg.RelayPingInterval,
		lifetime:        cfg.Lifetime,
	}
	if s.requestTimeout <= 0 {
		s.requestTimeout = defaultRequestTimeout
	}
	if s.peerWaitTimeout <= 0 {
		s.peerWaitTimeout = defaultRelayPeerTimeout
	}
	if s.pingInterval <= 0 {
		s.pingInterval = defaultRelayPingInterval
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

const (
	// auditConcurrentWrites bounds in-flight audit rows, so a burst adds no unbounded
	// goroutines competing for the connection pool. It matches the telemetry persistence slots.
	auditConcurrentWrites = 4

	// auditWriteTimeout bounds one audit row's write so a slot is not held indefinitely.
	auditWriteTimeout = 5 * time.Second
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

	// Liveness reports only that the process is up, so a Postgres or Redis blip cannot
	// restart the pod; readiness is /api/v1/health.
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
		apiRouter.Use(RateLimiter(100, 200, s.trustedProxies))

		HandlerWithOptions(strictHandler, ChiServerOptions{
			BaseRouter: apiRouter,
			Middlewares: []MiddlewareFunc{
				s.oapiAuthMiddleware(),
				AuthRateLimiter(10, 20, s.trustedProxies),
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

// logHTTPIssue logs a request-scoped problem with the path redacted and the request's
// correlation ID attached.
func (s *Server) logHTTPIssue(level slog.Level, msg string, r *http.Request, err error) {
	s.logger.Log(r.Context(), level, msg, append([]any{
		"error", err,
		"path", redactLogPath(r.URL.Path),
	}, correlationAttrs(r)...)...)
}

// registerSPA installs static file serving with an index.html fallback. os.OpenRoot
// rejects paths that escape s.webDir through "..", absolute paths or symlinks.
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

// serveStaticFile serves the request path from webRoot. handled is false when the caller
// falls back to the SPA index; otherwise served says whether the file was written.
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
		// os.Root.Open reports ErrNotExist at the first missing component, before it can see an
		// escape, so the ".." check keeps traversal paths out of the SPA fallback.
		return false, false
	default:
		// Traversal, permission and symlink-escape errors answer 404.
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

	slots := s.auditWriteSlots()
	select {
	case slots <- struct{}{}:
	default:
		// With every slot busy the store is slower than the requests, so the write is shed and
		// counted, which keeps goroutines off the connection pool.
		s.observeAuditWrite("shed")
		s.logger.Warn("audit log write shed: every write slot is busy", "action", action)
		return
	}

	go func() {
		defer func() { <-slots }()
		ctx, cancel := context.WithTimeout(auditCtx, auditWriteTimeout)
		defer cancel()
		if err := s.audit.Write(ctx, &audit.Event{
			UserID:    userID,
			Action:    action,
			Target:    target,
			Details:   details,
			CreatedAt: time.Now(),
		}); err != nil {
			s.observeAuditWrite("failed")
			s.logger.Error("audit log write failed", "action", action, "error", err)
			return
		}
		s.observeAuditWrite("written")
	}()
}

// auditWriteSlots is the bound, created on first use.
func (s *Server) auditWriteSlots() chan struct{} {
	s.auditSlotsOnce.Do(func() {
		if s.auditSlots == nil {
			s.auditSlots = make(chan struct{}, auditConcurrentWrites)
		}
	})
	return s.auditSlots
}

// observeAuditWrite records one audited action's outcome: written, failed or shed.
func (s *Server) observeAuditWrite(result string) {
	if s.metrics != nil {
		s.metrics.ObserveAuditWrite(result)
	}
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
