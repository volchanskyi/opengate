// Command meshserver reads flags, environment, listeners and signals and hands the resolved
// values to the composition root in internal/app, which assembles the server.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// databaseOpenBudget bounds the initial connection to Postgres.
const databaseOpenBudget = 30 * time.Second

// shutdownBudget bounds the graceful drain of in-flight HTTP requests.
const shutdownBudget = 10 * time.Second

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	internalListen := flag.String("internal-listen", ":8081", "cluster-only listen address for metrics and profiling")
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

	secret := firstNonEmpty(*jwtSecret, os.Getenv("JWT_SECRET"))
	if secret == "" {
		logger.Error("jwt secret is required: set --jwt-secret or JWT_SECRET")
		os.Exit(1)
	}

	pgURL := firstNonEmpty(*databaseURL, os.Getenv("DATABASE_URL"))
	if pgURL == "" {
		logger.Error("database URL is required: set --database-url or DATABASE_URL")
		os.Exit(1)
	}

	pgCtx, pgCancel := context.WithTimeout(context.Background(), databaseOpenBudget)
	store, err := db.NewPostgresStore(pgCtx, pgURL)
	pgCancel()
	if err != nil {
		logger.Error("open postgres database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	logger.Info("database opened", "backend", "postgres")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assembly, err := app.Build(ctx, app.Config{
		Store:              store,
		DataDir:            *dataDir,
		JWTSecret:          secret,
		Logger:             logger,
		VictoriaMetricsURL: firstNonEmpty(*victoriaMetricsURL, os.Getenv("OPENGATE_VICTORIAMETRICS_URL")),
		VMDeleteAuthKey:    os.Getenv("OPENGATE_VM_DELETE_AUTH_KEY"),
		Namespace:          os.Getenv("OPENGATE_NAMESPACE"),
		AMTUser:            *amtUser,
		AMTPass:            *amtPass,
		VAPIDContact:       *vapidContact,
		GitHubRepo:         os.Getenv("OPENGATE_GITHUB_REPO"),
		BaseURL:            os.Getenv("OPENGATE_BASE_URL"),
		QuicHost:           os.Getenv("OPENGATE_QUIC_HOST"),
		TrustedProxies:     os.Getenv("OPENGATE_TRUSTED_PROXIES"),
		WebDir:             *webDir,
		InternalListen:     *internalListen,
	})
	if err != nil {
		logger.Error("assemble server", "error", err)
		os.Exit(1)
	}

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           assembly.API,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	if err := assembly.StartBackgroundWorkers(ctx, productionSchedule); err != nil {
		logger.Error("start background workers", "error", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	serveBackground("HTTP server", httpSrv, logger)
	serveBackground("internal HTTP server", assembly.Internal, logger)

	go func() {
		logger.Info("agent QUIC server starting", "addr", *quicListen)
		if err := assembly.Agents.ListenAndServe(ctx, *quicListen); err != nil {
			logger.Error("agent server error", "error", err)
		}
	}()

	go func() {
		logger.Info("MPS server starting", "addr", *mpsListen)
		if err := assembly.MPS.ListenAndServe(ctx, *mpsListen); err != nil {
			logger.Error("MPS server error", "error", err)
		}
	}()

	<-done
	logger.Info("shutting down")

	// Cancelling the assembly's context stops the agent QUIC server and tells
	// every parked relay handler to close its session.
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer shutdownCancel()

	// Relay sessions drain before Shutdown, which cannot see them: websocket.Accept hijacked each
	// connection, so Shutdown alone returns while those sessions are still open.
	if err := assembly.Relay.WaitForDrain(shutdownCtx); err != nil {
		logger.Warn("relay sessions still live at the shutdown deadline",
			"sessions", assembly.Relay.ActiveSessionCount(), "error", err)
	}

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown error", "error", err)
	}

	if err := assembly.Internal.Shutdown(shutdownCtx); err != nil {
		logger.Error("internal HTTP shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// firstNonEmpty returns the first non-empty argument; each caller passes the flag before its
// environment variable, so the flag takes precedence.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// serveBackground runs srv.ListenAndServe in a goroutine and exits the process on any error
// other than http.ErrServerClosed.
func serveBackground(name string, srv *http.Server, logger *slog.Logger) {
	go func() {
		logger.Info(name+" starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error(name+" error", "error", err)
			os.Exit(1)
		}
	}()
}

// productionSchedule sets how often each periodic worker runs in the running server.
var productionSchedule = app.BackgroundSchedule{
	// Gauges refresh faster than a short load run, so a burst inside the window is still seen.
	Gauges: app.ProductionGaugeInterval,

	// The database size moves slowly and its query is costly, so it refreshes below scrape rate.
	DBSize: 60 * time.Second,

	// Investigation aggregates scan tables that only grow, so a minute-old count is enough.
	Investigations: time.Minute,

	// The orphan-series sweep backs up a purge that already ran, so it runs hourly.
	Reconcile: time.Hour,

	// The grace period outlasts the gap between issuing a token and connecting with it.
	// It is also the worst-case lag before a session orphaned by a restart leaves the device page.
	SessionSweep: time.Minute,
	SessionGrace: 5 * time.Minute,

	// The sweep interval bounds how long a quiet room stays in the triage queue.
	IncidentSweep: 5 * time.Minute,

	// Alerts, their evidence and their rooms are kept for one year.
	RetentionHorizon: 365 * 24 * time.Hour,

	// Four passes a day keep rows close to the horizon and each pass small.
	RetentionSweep: 6 * time.Hour,
}
