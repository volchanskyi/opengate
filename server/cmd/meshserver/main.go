// Command meshserver reads the world — flags, environment, listeners, signals
// — and hands the resolved values to the composition root in internal/app,
// which assembles the product. Nothing is wired here.
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

	// PostgreSQL is required — read from flag or DATABASE_URL env.
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

	// Use a cancellable context for graceful shutdown of all servers.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	assembly, err := app.Build(ctx, app.Config{
		Store:              store,
		DataDir:            *dataDir,
		JWTSecret:          secret,
		Logger:             logger,
		VictoriaMetricsURL: firstNonEmpty(*victoriaMetricsURL, os.Getenv("OPENGATE_VICTORIAMETRICS_URL")),
		VMDeleteAuthKey:    os.Getenv("OPENGATE_VM_DELETE_AUTH_KEY"),
		AMTUser:            *amtUser,
		AMTPass:            *amtPass,
		VAPIDContact:       *vapidContact,
		GitHubRepo:         os.Getenv("OPENGATE_GITHUB_REPO"),
		BaseURL:            os.Getenv("OPENGATE_BASE_URL"),
		QuicHost:           os.Getenv("OPENGATE_QUIC_HOST"),
		WebDir:             *webDir,
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

	cancel() // Stop the agent QUIC server

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

// firstNonEmpty returns the first of its arguments that carries a value. Every
// setting this process reads comes from a flag or an environment variable, in
// that order, so the choice is made once here rather than at each site.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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

// productionSchedule is how often each periodic worker runs in the running
// server. The product knows how to start its workers; how often they should run
// is this binary's choice, so the cadence is stated here and passed in.
var productionSchedule = app.BackgroundSchedule{
	// The runtime gauges — sessions, connected agents, connected AMT devices,
	// signaling, connection-pool occupancy — are read often because a load run
	// is short. The lag an observer adds is its own refresh plus the scrape
	// behind it, and a burst that starts and finishes inside that window leaves
	// every gauge reading the number it held before the burst began, so a run
	// that connected a thousand agents can be recorded as a server that saw
	// none. These are in-memory counts, so reading them this often costs
	// nothing worth saving.
	Gauges: 5 * time.Second,

	// The database's on-disk size moves slowly and the query is not free, so it
	// is read far less often than it is scraped.
	DBSize: 60 * time.Second,

	// The platform's own view of the investigation tables is deliberately
	// slower than the scrape: these are aggregates over tables that only grow,
	// and a triage queue moves at the speed of people rather than of requests,
	// so a minute-old count answers every question the gauges are asked.
	Investigations: time.Minute,

	// The orphan-series sweep is defense in depth behind a purge that already
	// ran, so it runs on the hour rather than on the minute.
	Reconcile: time.Hour,

	// A session row survives without the relay holding its token for long
	// enough to outlast the gap between issuing a token and connecting with it
	// — a live session is spared by the keep-list however old it gets — so the
	// grace period doubles as the worst-case lag before a session orphaned by a
	// process restart disappears from the device page.
	SessionSweep: time.Minute,
	SessionGrace: 5 * time.Minute,

	// How promptly a room that has gone quiet leaves the triage queue, and not
	// whether it does: the hold itself is the rule's own grouping window, and an
	// alert arriving after that window closes the lapsed room on its way past.
	IncidentSweep: 5 * time.Minute,

	// A year is how long an alert, its evidence and the room it folded into are
	// kept. Erasure already runs off a machine or a customer the moment either
	// is purged; this is the other axis, and it is what makes the declared
	// period the one the tables actually observe.
	RetentionHorizon: 365 * 24 * time.Hour,

	// Against a horizon of a year, the cadence decides only two things: how far
	// past the horizon a row can sit before it goes, and how much one pass has
	// to remove. Four passes a day keeps both small, and a caught-up pass is an
	// indexed range scan that finds nothing.
	RetentionSweep: 6 * time.Hour,
}
