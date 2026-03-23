package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/mps"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	quicListen := flag.String("quic-listen", ":9090", "QUIC listen address for agent connections")
	mpsListen := flag.String("mps-listen", ":4433", "MPS TLS listen address for Intel AMT CIRA connections")
	dataDir := flag.String("data-dir", "./data", "directory for database and certificates")
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

	if err := os.MkdirAll(*dataDir, 0755); err != nil {
		logger.Error("create data dir", "error", err)
		os.Exit(1)
	}

	store, err := db.NewSQLiteStore(filepath.Join(*dataDir, "opengate.db"))
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Prometheus metrics
	metricsRegistry := appmetrics.NewRegistry()
	appMetrics := appmetrics.NewMetrics(metricsRegistry)
	instrumentedStore := appmetrics.NewInstrumentedStore(store, appMetrics)

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
	notifier := notifications.NewPushNotifier(instrumentedStore, vapidPriv, vapidPub, *vapidContact, logger)

	// Environment overrides
	githubRepo := os.Getenv("OPENGATE_GITHUB_REPO")
	baseURL := os.Getenv("OPENGATE_BASE_URL")
	quicHost := os.Getenv("OPENGATE_QUIC_HOST")

	// Create relay and agent server
	agentRelay := relay.NewRelay()
	agentRelay.OnSessionEnd = func(token protocol.SessionToken) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.DeleteAgentSession(ctx, string(token)); err != nil {
			logger.Error("cleanup session on disconnect", "error", err, "token_prefix", protocol.RedactToken(string(token)))
		}
	}
	agentSrv := agentapi.NewAgentServer(certMgr, instrumentedStore, agentRelay, notifier, quicHost, logger)

	// Initialize update signing keys and manifest store
	signingKeys, err := updater.LoadOrGenerateSigningKeys(*dataDir)
	if err != nil {
		logger.Error("init update signing keys", "error", err)
		os.Exit(1)
	}
	manifestStore := updater.NewManifestStore(*dataDir)

	mpsSrv := mps.NewServer(certMgr, instrumentedStore, logger)
	amtSvc := amt.NewService(mpsSrv, *amtUser, *amtPass, logger)

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
	srv := api.NewServer(api.ServerConfig{
		Store:     instrumentedStore,
		JWT:       jwtCfg,
		Agents:    agentSrv,
		AMT:       amtSvc,
		Cert:      certMgr,
		Relay:     agentRelay,
		Signaling: sigTracker,
		Notifier:  notifier,
		Signing:    signingKeys,
		Manifests:  manifestStore,
		GitHubRepo: githubRepo,
		BaseURL:    baseURL,
		QuicHost:   quicHost,
		Logger:     logger,
		WebDir:     *webDir,
		MetricsRegistry: metricsRegistry,
		Metrics:         appMetrics,
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
		SignalingSuccesses:   sigTracker.SuccessCount,
		SignalingFailures:    sigTracker.FailureCount,
	}, 15*time.Second)
	go appmetrics.StartDBSizeUpdater(ctx, appMetrics, store.DB(), logger, 60*time.Second)

	// Periodically sync agent manifests from GitHub releases (default: every hour).
	if githubRepo != "" {
		go updater.StartPeriodicSync(ctx, githubRepo, 0, signingKeys, manifestStore, logger)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("HTTP server starting", "addr", *listen)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

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
