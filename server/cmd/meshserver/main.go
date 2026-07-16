package main

import (
	"context"
	"database/sql"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/correlate"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	quicListen := flag.String("quic-listen", ":9090", "QUIC listen address for agent connections")
	mpsListen := flag.String("mps-listen", ":4433", "MPS TLS listen address for Intel AMT CIRA connections")
	dataDir := flag.String("data-dir", "./data", "directory for database and certificates")
	databaseURL := flag.String("database-url", "", "PostgreSQL connection URL (or DATABASE_URL env); required")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (or JWT_SECRET env)")
	vapidContact := flag.String("vapid-contact", "", "VAPID contact email for web push (optional)")
	webDir := flag.String("web-dir", "", "directory containing SPA static assets (optional)")
	victoriaMetricsURL := flag.String("victoriametrics-url", "", "VictoriaMetrics base URL (or OPENGATE_VICTORIAMETRICS_URL env; optional)")
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

	// The audit module owns its outbound persistence port. It is wired against the
	// same connection pool as the main store; instrumented so audit calls
	// land on the same db_query_* metrics as before the extraction.
	auditRepo := audit.NewInstrumented(audit.NewPostgres(store.DB()), appMetrics)

	// The update module owns the DeviceUpdate and EnrollmentToken aggregates.
	// Same pattern as audit: leaf module, Postgres adapter against the shared
	// pool, Instrumented decorator preserves db_query_* metric continuity.
	deviceUpdatesRepo := updater.NewInstrumentedDeviceUpdates(updater.NewPostgresDeviceUpdates(store.DB()), appMetrics)
	enrollmentRepo := updater.NewInstrumentedEnrollment(updater.NewPostgresEnrollment(store.DB()), appMetrics)
	securityGroupsRepo := auth.NewInstrumentedSecurityGroups(auth.NewPostgresSecurityGroups(store.DB()), appMetrics)
	devicesRepo := device.NewInstrumentedDevices(device.NewPostgresDevices(store.DB()), appMetrics)
	groupsRepo := device.NewInstrumentedGroups(device.NewPostgresGroups(store.DB()), appMetrics)
	hardwareRepo := device.NewInstrumentedHardware(device.NewPostgresHardware(store.DB()), appMetrics)
	webPushRepo := notifications.NewInstrumentedWebPush(notifications.NewPostgresWebPush(store.DB()), appMetrics)
	amtRepo := amt.NewInstrumented(amt.NewPostgresAMTDevices(store.DB()), appMetrics)
	sessionsRepo := session.NewInstrumented(session.NewPostgresSessions(store.DB()), appMetrics)
	usersRepo := auth.NewInstrumentedUsers(auth.NewPostgresUsers(store.DB()), appMetrics)
	processesRepo := telemetry.NewPostgresProcessRepository(store.DB())
	inventoryRepo := inventory.NewPostgresInventoryRepository(store.DB())

	vmURL := *victoriaMetricsURL
	if vmURL == "" {
		vmURL = os.Getenv("OPENGATE_VICTORIAMETRICS_URL")
	}
	// Data-lifecycle stores back right-to-be-forgotten purges. They live on
	// non-tenant tables, so they exist regardless of whether numeric telemetry
	// (VictoriaMetrics) is enabled; the agent server warms its in-memory deny-list
	// from the tombstone store so a purged device stays rejected across restarts.
	tombstoneStore := lifecycle.NewTombstoneStore(store.DB())
	jobStore := lifecycle.NewJobStore(store.DB())

	var telemetryWriter telemetry.NumericWriter
	var correlationEngine api.CorrelationRanker
	var metricsReader api.MetricsReader
	var seriesPurger lifecycle.SeriesPurger
	var seriesInventory lifecycle.SubjectLister
	if vmURL != "" {
		vmClient := telemetry.NewVMClient(vmURL, nil)
		if key := os.Getenv("OPENGATE_VM_DELETE_AUTH_KEY"); key != "" {
			vmClient = vmClient.WithDeleteAuthKey(key)
		}
		telemetryWriter = vmClient
		metricsReader = vmClient
		seriesPurger = vmClient
		seriesInventory = vmClient
		engine, err := correlate.NewEngine(correlate.Config{Fetcher: correlate.NewVMFetcher(vmClient)})
		if err != nil {
			logger.Error("init correlation engine", "error", err)
			os.Exit(1)
		}
		correlationEngine = engine
		logger.Info("edge sentinel telemetry writer enabled", "victoriametrics_url", vmURL)
	} else {
		logger.Warn("edge sentinel numeric telemetry disabled: set --victoriametrics-url or OPENGATE_VICTORIAMETRICS_URL")
	}

	// Reset stale online statuses from a prior run via the device repository.
	if err := devicesRepo.ResetAllStatuses(dbtx.WithDefaultTenant(context.Background(), false)); err != nil {
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

	// Create relay and agent server. The relay records session lifecycle through
	// its SessionRegistry port; the default in-process adapter keeps that metadata
	// local while both connection sides pair in this process.
	agentRelay := relay.NewRelay(logger)
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
		DeviceUpdates: deviceUpdatesRepo,
		Telemetry:     telemetryWriter,
		Processes:     processesRepo,
		Inventory:     inventoryRepo,
		Relay:         agentRelay,
		Notifier:      notifier,
		Metrics:       appMetrics,
		QuicHost:      quicHost,
		Tombstones:    tombstoneStore,
		Logger:        logger,
	})

	// Wire the right-to-be-forgotten purge orchestrator. It needs VictoriaMetrics
	// to delete numeric series, so it is enabled only alongside numeric telemetry;
	// without it, device deletion falls back to the plain Postgres delete. A
	// periodic reconciliation sweep garbage-collects any orphaned series.
	purger, purgeJobs, reconciler := buildPurgeOrchestrator(purgeDeps{
		agentSrv:        agentSrv,
		db:              store.DB(),
		tombstones:      tombstoneStore,
		jobs:            jobStore,
		seriesPurger:    seriesPurger,
		seriesInventory: seriesInventory,
		logger:          logger,
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
		Inventory:             inventoryRepo,
		WebPush:               webPushRepo,
		NotificationsHandlers: notifHandlers,
		AMTDevices:            amtRepo,
		AMTHandlers:           amtHandlers,
		Sessions:              sessionsRepo,
		Users:                 usersRepo,
		JWT:                   jwtCfg,
		Agents:                agentControlGetter{srv: agentSrv},
		AMT:                   amtSvc,
		Cert:                  certMgr,
		Correlate:             correlationEngine,
		TelemetryReader:       metricsReader,
		Purger:                purger,
		PurgeJobs:             purgeJobs,
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
	}, 15*time.Second)
	go appmetrics.StartDBSizeUpdater(ctx, appMetrics, store, logger, 60*time.Second)

	// Periodically garbage-collect any orphaned telemetry series (defense in depth
	// against a purge that partially failed). A no-op when purging is disabled.
	go startReconcileLoop(ctx, reconciler, logger)

	// Periodically sync agent manifests from GitHub releases (default: every hour).
	if githubRepo != "" {
		go updater.StartPeriodicSync(ctx, githubRepo, 0, signingKeys, manifestStore, logger)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	serveBackground("HTTP server", httpSrv, logger)

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

	logger.Info("server stopped")
}

// agentControlGetter adapts *agentapi.AgentServer to api.AgentGetter. The server's
// getters return the concrete *agentapi.AgentConn while the api port speaks the
// api.AgentControl interface; Go has no covariant return types and agentapi cannot
// import api (that would cycle), so the composition root bridges the two here. A
// missing agent's typed-nil *AgentConn is converted to an interface nil so the
// handlers' `ac == nil` checks still fire.
type agentControlGetter struct {
	srv *agentapi.AgentServer
}

func (g agentControlGetter) GetAgent(deviceID db.DeviceID) api.AgentControl {
	ac := g.srv.GetAgent(deviceID)
	if ac == nil {
		return nil // typed-nil *AgentConn → interface nil
	}
	return ac
}

func (g agentControlGetter) ListConnectedAgents() []api.AgentControl {
	conns := g.srv.ListConnectedAgents()
	out := make([]api.AgentControl, 0, len(conns))
	for _, ac := range conns {
		out = append(out, ac)
	}
	return out
}

func (g agentControlGetter) DeregisterAgent(ctx context.Context, deviceID db.DeviceID) {
	g.srv.DeregisterAgent(ctx, deviceID)
}

// purgeDeps groups the dependencies buildPurgeOrchestrator wires, keeping the
// call site in main readable.
type purgeDeps struct {
	agentSrv        *agentapi.AgentServer
	db              *sql.DB
	tombstones      *lifecycle.TombstoneStore
	jobs            *lifecycle.JobStore
	seriesPurger    lifecycle.SeriesPurger
	seriesInventory lifecycle.SubjectLister
	logger          *slog.Logger
}

// buildPurgeOrchestrator wires the right-to-be-forgotten purge orchestrator plus
// its reconciliation sweep. It needs VictoriaMetrics to delete numeric series, so
// it returns nils when numeric telemetry is disabled and device deletion falls
// back to the plain Postgres delete. On success it warms the agent deny-list and
// resumes any purge a prior crash interrupted before the server starts serving.
func buildPurgeOrchestrator(d purgeDeps) (api.DevicePurger, api.PurgeJobReader, *lifecycle.Reconciler) {
	if d.seriesPurger == nil {
		return nil, nil, nil
	}
	orchestrator := lifecycle.NewOrchestrator(lifecycle.OrchestratorConfig{
		Tombstones: d.tombstones,
		Jobs:       d.jobs,
		Series:     d.seriesPurger,
		PG:         lifecycle.NewPostgresPurger(d.db),
		Edge:       d.agentSrv,
		Logger:     d.logger,
	})
	reconciler := lifecycle.NewReconciler(d.seriesInventory, d.seriesPurger, lifecycle.NewPostgresPurger(d.db), d.logger)

	startupCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := d.agentSrv.WarmTombstones(startupCtx); err != nil {
		d.logger.Error("warm tombstone deny-list", "error", err)
	}
	if err := orchestrator.Resume(startupCtx); err != nil {
		d.logger.Error("resume interrupted purges", "error", err)
	}
	return orchestrator, d.jobs, reconciler
}

// reconcileInterval is how often the orphan-series sweep runs.
const reconcileInterval = time.Hour

// startReconcileLoop runs the reconciliation sweep on a ticker until ctx is
// cancelled. A nil reconciler (purging disabled) returns immediately.
func startReconcileLoop(ctx context.Context, reconciler *lifecycle.Reconciler, logger *slog.Logger) {
	if reconciler == nil {
		return
	}
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purged, err := reconciler.Sweep(ctx)
			if err != nil {
				logger.Error("reconcile sweep failed", "error", err)
				continue
			}
			if purged > 0 {
				logger.Warn("reconcile sweep purged orphan telemetry", "count", purged)
			}
		}
	}
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
