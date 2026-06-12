package main

import (
	"context"
	"flag"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	quicListen := flag.String("quic-listen", ":9090", "QUIC listen address for agent connections")
	mpsListen := flag.String("mps-listen", ":4433", "MPS TLS listen address for Intel AMT CIRA connections")
	internalListen := flag.String("internal-listen", ":9091", "internal cross-server relay listen address (or OPENGATE_INTERNAL_LISTEN env); never exposed via ingress")
	dataDir := flag.String("data-dir", "./data", "directory for database and certificates")
	databaseURL := flag.String("database-url", "", "PostgreSQL connection URL (or DATABASE_URL env); required")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (or JWT_SECRET env)")
	vapidContact := flag.String("vapid-contact", "", "VAPID contact email for web push (optional)")
	webDir := flag.String("web-dir", "", "directory containing SPA static assets (optional)")
	amtUser := flag.String("amt-user", "admin", "AMT WSMAN username for device management")
	amtPass := flag.String("amt-pass", "", "AMT WSMAN password for device management")
	flag.Parse()

	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	secret := *jwtSecret
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	if secret == "" {
		logger.Error("jwt secret is required: set --jwt-secret or JWT_SECRET")
		os.Exit(1)
	}
	if len(secret) < 32 {
		logger.Error("jwt secret must be at least 32 characters")
		os.Exit(1)
	}

	if err := os.MkdirAll(*dataDir, 0750); err != nil {
		logger.Error("create data dir", "error", err)
		os.Exit(1)
	}

	// PostgreSQL is required — read from flag or DATABASE_URL env.
	pgURL := *databaseURL
	if pgURL == "" {
		pgURL = os.Getenv("DATABASE_URL")
	}
	if pgURL == "" {
		logger.Error("database URL is required: set --database-url or DATABASE_URL")
		os.Exit(1)
	}

	pgCtx, pgCancel := context.WithTimeout(context.Background(), 30*time.Second)
	store, err := db.NewPostgresStore(pgCtx, pgURL)
	pgCancel()
	if err != nil {
		logger.Error("open postgres database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	logger.Info("database opened", "backend", "postgres")

	// Prometheus metrics
	metricsRegistry := appmetrics.NewRegistry()
	appMetrics := appmetrics.NewMetrics(metricsRegistry)

	// Audit module owns its own outbound port (ADR-021). Wired against the
	// same connection pool as the main store; instrumented so audit calls
	// land on the same db_query_* metrics as before the extraction.
	auditRepo := audit.NewInstrumented(audit.NewPostgres(store.DB()), appMetrics)

	// Update module owns DeviceUpdate + EnrollmentToken aggregates (ADR-021).
	// Same pattern as audit: leaf module, Postgres adapter against the shared
	// pool, Instrumented decorator preserves db_query_* metric continuity.
	deviceUpdatesRepo := updater.NewInstrumentedDeviceUpdates(updater.NewPostgresDeviceUpdates(store.DB()), appMetrics)
	enrollmentRepo := updater.NewInstrumentedEnrollment(updater.NewPostgresEnrollment(store.DB()), appMetrics)
	securityGroupsRepo := auth.NewInstrumentedSecurityGroups(auth.NewPostgresSecurityGroups(store.DB()), appMetrics)
	devicesRepo := device.NewInstrumentedDevices(device.NewPostgresDevices(store.DB()), appMetrics)
	groupsRepo := device.NewInstrumentedGroups(device.NewPostgresGroups(store.DB()), appMetrics)
	hardwareRepo := device.NewInstrumentedHardware(device.NewPostgresHardware(store.DB()), appMetrics)
	deviceLogsRepo := device.NewInstrumentedLogs(device.NewPostgresLogs(store.DB()), appMetrics)
	webPushRepo := notifications.NewInstrumentedWebPush(notifications.NewPostgresWebPush(store.DB()), appMetrics)
	amtRepo := amt.NewInstrumented(amt.NewPostgresAMTDevices(store.DB()), appMetrics)
	sessionsRepo := session.NewInstrumented(session.NewPostgresSessions(store.DB()), appMetrics)
	usersRepo := auth.NewInstrumentedUsers(auth.NewPostgresUsers(store.DB()), appMetrics)

	// Reset stale online statuses from a prior run via the device repository.
	if err := devicesRepo.ResetAllStatuses(context.Background()); err != nil {
		logger.Error("reset device statuses on startup", "error", err)
		os.Exit(1)
	}

	certMgr, err := cert.NewManager(*dataDir)
	if err != nil {
		logger.Error("init cert manager", "error", err)
		os.Exit(1)
	}

	jwtCfg := &auth.JWTConfig{
		Secret:   secret,
		Issuer:   "opengate",
		Duration: 24 * time.Hour,
	}

	// Initialize VAPID keys and push notifier
	vapidPriv, vapidPub, err := notifications.LoadOrGenerateVAPID(*dataDir)
	if err != nil {
		logger.Error("init VAPID keys", "error", err)
		os.Exit(1)
	}
	notifier := notifications.NewPushNotifier(webPushRepo, vapidPriv, vapidPub, *vapidContact, logger)

	// Environment overrides
	githubRepo := os.Getenv("OPENGATE_GITHUB_REPO")
	baseURL := os.Getenv("OPENGATE_BASE_URL")
	quicHost := os.Getenv("OPENGATE_QUIC_HOST")

	// Create relay and agent server. The relay tracks session affinity through
	// the SessionRegistry port (ADR-023). REGISTRY_BACKEND selects the
	// adapter: "inprocess" (default, single-server) or "redis" (multi-server
	// pool with cross-server affinity). serverID identifies this node in the
	// relay pool — hostname by default, overridable for k8s pods.
	sessionRegistry, registryCloser := initSessionRegistry(logger)
	defer func() { _ = registryCloser.Close() }()
	// Cross-server proxy (Phase 13b PR-C, ADR-023): when a distributed registry
	// reports a foreign owner, the relay splices the session through the owner's
	// internal listener instead of pairing locally. All pods are homogeneous, so
	// the dialer reuses this node's internal port to reach any peer (pod IP via
	// the Downward API → OPENGATE_SERVER_ID). The secret is optional
	// defense-in-depth on top of the private-network boundary.
	internalAddr := envOr("OPENGATE_INTERNAL_LISTEN", *internalListen)
	serverID := resolveServerID()
	proxySecret := os.Getenv("OPENGATE_PROXY_SECRET")
	peerDialer := api.NewHTTPPeerDialer(serverID, portOf(internalAddr), proxySecret, logger)
	agentRelay := relay.NewRelay(logger, buildRelayOptions(sessionRegistry, serverID, peerDialer, logger)...)
	agentRelay.OnSessionEnd = func(token protocol.SessionToken) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := sessionsRepo.Delete(ctx, string(token)); err != nil {
			logger.Error("cleanup session on disconnect", "error", err, "token_prefix", protocol.RedactToken(string(token)))
		}
	}
	agentSrv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          certMgr,
		Devices:       devicesRepo,
		Hardware:      hardwareRepo,
		DeviceLogs:    deviceLogsRepo,
		DeviceUpdates: deviceUpdatesRepo,
		Relay:         agentRelay,
		Notifier:      notifier,
		QuicHost:      quicHost,
		Logger:        logger,
	})

	// Initialize update signing keys and manifest store
	signingKeys, err := updater.LoadOrGenerateSigningKeys(*dataDir)
	if err != nil {
		logger.Error("init update signing keys", "error", err)
		os.Exit(1)
	}
	manifestStore := updater.NewManifestStore(*dataDir)

	mpsSrv := transport.NewServer(certMgr, amtRepo, logger)
	amtSvc := amt.NewService(mpsSrv, *amtUser, *amtPass, logger)

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
	auditHandlers := audit.NewHandlers(auditRepo)
	amtHandlers := amt.NewHandlers(amtRepo, amtSvc)
	notifHandlers := notifications.NewHandlers(webPushRepo, notifier)

	srv := api.NewServer(api.ServerConfig{
		Store:                 store,
		Audit:                 auditRepo,
		AuditHandlers:         auditHandlers,
		DeviceUpdates:         deviceUpdatesRepo,
		Enrollment:            enrollmentRepo,
		SecurityGroups:        securityGroupsRepo,
		Devices:               devicesRepo,
		Groups:                groupsRepo,
		Hardware:              hardwareRepo,
		DeviceLogs:            deviceLogsRepo,
		WebPush:               webPushRepo,
		NotificationsHandlers: notifHandlers,
		AMTDevices:            amtRepo,
		AMTHandlers:           amtHandlers,
		Sessions:              sessionsRepo,
		Users:                 usersRepo,
		JWT:                   jwtCfg,
		Agents:                agentSrv,
		AMT:                   amtSvc,
		Cert:                  certMgr,
		Relay:                 agentRelay,
		Signaling:             sigTracker,
		Notifier:              notifier,
		Signing:               signingKeys,
		Manifests:             manifestStore,
		GitHubRepo:            githubRepo,
		BaseURL:               baseURL,
		QuicHost:              quicHost,
		Logger:                logger,
		WebDir:                *webDir,
		MetricsRegistry:       metricsRegistry,
		Metrics:               appMetrics,
	})

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Internal cross-server relay listener (ADR-023): private port for proxied
	// peer connections, never fronted by the public router/ingress.
	internalSrv := &http.Server{
		Addr:              internalAddr,
		Handler:           api.NewInternalRelayServer(agentRelay, serverID, proxySecret, logger).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Use a cancellable context for graceful shutdown of all servers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start metrics gauge updaters
	go appmetrics.StartGaugeUpdater(ctx, appMetrics, appmetrics.GaugeSource{
		ActiveSessions:      agentRelay.ActiveSessionCount,
		ConnectedAgents:     agentSrv.ConnectedAgentCount,
		ConnectedMPSDevices: amtSvc.ConnectedDeviceCount,
		SignalingSuccesses:  sigTracker.SuccessCount,
		SignalingFailures:   sigTracker.FailureCount,
		RegistryUp:          agentRelay.RegistryUp,
	}, 15*time.Second)
	go appmetrics.StartDBSizeUpdater(ctx, appMetrics, store, logger, 60*time.Second)
	// Probe the session registry every 5s so the relay can drain new sessions
	// (degraded mode) and the opengate_registry_up gauge stays fresh (ADR-023).
	go agentRelay.MonitorRegistryHealth(ctx, 5*time.Second)

	// Periodically sync agent manifests from GitHub releases (default: every hour).
	if githubRepo != "" {
		go updater.StartPeriodicSync(ctx, githubRepo, 0, signingKeys, manifestStore, logger)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	serveBackground("HTTP server", httpSrv, logger)
	serveBackground("internal relay server", internalSrv, logger)

	go func() {
		logger.Info("agent QUIC server starting", "addr", *quicListen)
		if err := agentSrv.ListenAndServe(ctx, *quicListen); err != nil {
			logger.Error("agent server error", "error", err)
		}
	}()

	go func() {
		logger.Info("MPS server starting", "addr", *mpsListen)
		if err := mpsSrv.ListenAndServe(ctx, *mpsListen); err != nil {
			logger.Error("MPS server error", "error", err)
		}
	}()

	<-done
	logger.Info("shutting down")

	cancel() // Stop the agent QUIC server

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown error", "error", err)
	}
	if err := internalSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("internal relay shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// envOr returns the environment value for key, or fallback when it is unset or
// empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// portOf extracts the port from a listen address, tolerating both the ":port"
// and bare "port" forms. The HTTPPeerDialer reuses this port to reach
// homogeneous peers on the flat cluster overlay (ADR-023).
func portOf(addr string) string {
	if _, port, err := net.SplitHostPort(addr); err == nil {
		return port
	}
	return strings.TrimPrefix(addr, ":")
}

// serveBackground starts srv.ListenAndServe in a goroutine, logging startup and
// treating any non-graceful failure as fatal (matching the public HTTP listener).
func serveBackground(name string, srv *http.Server, logger *slog.Logger) {
	go func() {
		logger.Info(name+" starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(name+" error", "error", err)
			os.Exit(1)
		}
	}()
}

// resolveServerID returns this node's stable identifier in the relay pool.
// OPENGATE_SERVER_ID wins (set per-pod under k8s); otherwise the OS hostname;
// otherwise a fixed fallback so the value is never empty (the registry rejects
// empty serverIDs).
func resolveServerID() string {
	if id := os.Getenv("OPENGATE_SERVER_ID"); id != "" {
		return id
	}
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		return hostname
	}
	return "meshserver"
}

// parsePositiveDuration interprets an optional Go-duration override (e.g. from
// OPENGATE_DEGRADED_THRESHOLD or OPENGATE_AFFINITY_TTL). It returns ok=true only
// for a parseable, strictly-positive duration; an empty, malformed, or
// non-positive value leaves the relay default in place (ok=false). These
// overrides exist so the multiserver e2e can shorten the 30s timers (degraded
// mode, affinity reclaim) without waiting them out in real time.
func parsePositiveDuration(v string) (time.Duration, bool) {
	if v == "" {
		return 0, false
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, false
	}
	return d, true
}

// buildRelayOptions assembles the relay.Option set for the agent relay: the
// session-registry + serverID binding, the cross-server peer dialer, and
// optional timer overrides — OPENGATE_DEGRADED_THRESHOLD (how long the registry
// must be unreachable before new sessions are refused) and OPENGATE_AFFINITY_TTL
// (how long a dead owner's affinity claim survives before reclaim). Both are used
// by the multiserver e2e to trip the relevant behavior quickly. Keeping this out
// of main keeps the entry point's cognitive complexity down.
func buildRelayOptions(reg relay.SessionRegistry, serverID string, dialer relay.PeerDialer, logger *slog.Logger) []relay.Option {
	opts := []relay.Option{
		relay.WithRegistry(reg, serverID),
		relay.WithPeerDialer(dialer),
	}
	if d, ok := parsePositiveDuration(os.Getenv("OPENGATE_DEGRADED_THRESHOLD")); ok {
		opts = append(opts, relay.WithDegradedThreshold(d))
		logger.Info("degraded-mode threshold overridden", "threshold", d.String())
	}
	if d, ok := parsePositiveDuration(os.Getenv("OPENGATE_AFFINITY_TTL")); ok {
		opts = append(opts, relay.WithAffinityTTL(d))
		logger.Info("affinity TTL overridden", "ttl", d.String())
	}
	return opts
}

// initSessionRegistry builds the SessionRegistry adapter selected by
// REGISTRY_BACKEND (see relay.SessionRegistryFromConfig): "inprocess" (default)
// or "redis", configured via REDIS_ADDR / REDIS_SENTINEL_ADDRS /
// REDIS_MASTER_NAME. A misconfiguration is fatal. The returned io.Closer
// releases backend resources on shutdown.
func initSessionRegistry(logger *slog.Logger) (relay.SessionRegistry, io.Closer) {
	backend := os.Getenv("REGISTRY_BACKEND")
	reg, closer, err := relay.SessionRegistryFromConfig(backend, relay.RedisConfig{
		Addr:          os.Getenv("REDIS_ADDR"),
		SentinelAddrs: splitCSV(os.Getenv("REDIS_SENTINEL_ADDRS")),
		MasterName:    os.Getenv("REDIS_MASTER_NAME"),
		Password:      os.Getenv("REDIS_PASSWORD"),
	})
	if err != nil {
		logger.Error("init session registry", "error", err, "backend", backend)
		os.Exit(1)
	}
	if backend == "" {
		backend = "inprocess"
	}
	logger.Info("session registry initialized", "backend", backend)
	return reg, closer
}

// splitCSV splits a comma-separated env value into trimmed, non-empty entries.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
