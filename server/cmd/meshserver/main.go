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
	"github.com/volchanskyi/opengate/server/internal/mps"
	"github.com/volchanskyi/opengate/server/internal/notifications"
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

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

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
	notifier := notifications.NewPushNotifier(store, vapidPriv, vapidPub, *vapidContact, logger)

	// Create relay and agent server
	agentRelay := relay.NewRelay()
	agentSrv := agentapi.NewAgentServer(certMgr, store, agentRelay, notifier, logger)

	// Initialize update signing keys and manifest store
	signingKeys, err := updater.LoadOrGenerateSigningKeys(*dataDir)
	if err != nil {
		logger.Error("init update signing keys", "error", err)
		os.Exit(1)
	}
	manifestStore := updater.NewManifestStore(*dataDir)
	githubRepo := os.Getenv("OPENGATE_GITHUB_REPO")

	mpsSrv := mps.NewServer(certMgr, store, logger)
	amtSvc := amt.NewService(mpsSrv, *amtUser, *amtPass, logger)

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
	srv := api.NewServer(api.ServerConfig{
		Store:     store,
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
		Logger:     logger,
		WebDir:     *webDir,
	})

	httpSrv := &http.Server{
		Addr:         *listen,
		Handler:      srv,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Use a cancellable context for graceful shutdown of all servers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Auto-sync agent manifests from GitHub releases on startup.
	if githubRepo != "" {
		go func() {
			syncCtx, syncCancel := context.WithTimeout(ctx, 30*time.Second)
			defer syncCancel()
			synced, err := updater.SyncFromGitHub(syncCtx, githubRepo, "", signingKeys, manifestStore)
			if err != nil {
				logger.Warn("github manifest sync failed", "repo", githubRepo, "error", err)
			} else {
				logger.Info("synced manifests from github", "repo", githubRepo, "count", len(synced))
			}
		}()
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
