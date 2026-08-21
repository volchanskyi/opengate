// Package app is the composition root: the one place a port is wired to an
// adapter. Everything the server is made of — every repository, every
// handler set, the API server, the agent server, the relay and the erasure
// orchestrator — is assembled here, from configuration values somebody else
// resolved.
//
// The split against cmd/meshserver is deliberate and load-bearing. This
// package assembles; it never reads flags or the environment, never calls
// os.Exit, and never opens a listener. That is what lets a test stand the
// whole product up and talk to it, and it is why there is exactly one wiring
// of ServerConfig rather than one per harness.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/amt/transport"
	"github.com/volchanskyi/opengate/server/internal/api"
	"github.com/volchanskyi/opengate/server/internal/audit"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/organization"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// minJWTSecretLen is the shortest signing secret the product accepts. Held
// here rather than at the flag, so a harness cannot assemble a product with a
// secret the shipped binary would refuse.
const minJWTSecretLen = 32

// jwtTokenLifetime is how long an issued operator token stays valid.
const jwtTokenLifetime = 24 * time.Hour

// Config is the resolved configuration Build assembles from. Every field is a
// value somebody else already worked out: no flag is parsed here and no
// environment variable is read here.
type Config struct {
	// Store is the open database. Its owner closes it; Build never does.
	Store *db.PostgresStore
	// DataDir holds the certificate authority, the VAPID keys, the update
	// signing keys and the manifest store.
	DataDir string
	// JWTSecret signs operator tokens. At least minJWTSecretLen bytes.
	JWTSecret string
	// Logger receives everything the assembly and the servers say.
	Logger *slog.Logger

	// VictoriaMetricsURL enables numeric telemetry and, with it, series
	// erasure. Empty leaves both off and device deletion falls back to the
	// plain Postgres delete.
	VictoriaMetricsURL string
	// VMDeleteAuthKey authorises the delete API of that metrics store.
	VMDeleteAuthKey string

	// AMTUser and AMTPass are the WSMAN credentials for Intel management
	// hardware.
	AMTUser string
	AMTPass string

	// VAPIDContact is the contact address browser push services are given.
	VAPIDContact string
	// GitHubRepo is the release feed agent manifests are synced from.
	GitHubRepo string
	// BaseURL is the public address the install script is written against.
	BaseURL string
	// QuicHost overrides the hostname an enrolling agent is told to dial.
	QuicHost string
	// WebDir holds the single-page application's static assets.
	WebDir string

	// AMTOperator replaces the management service the API surface is given.
	// Intel management hardware answers on its own network path, which no test
	// host can stand up, so this is the one edge a harness may stand in for.
	// Nil — the shipped binary's case — gives the API the real service over the
	// MPS listener.
	AMTOperator amt.Operator
}

// Assembly is the wired product. It holds every component the process needs
// to serve requests and to run its periodic work; starting listeners and
// loops belongs to whoever built it.
type Assembly struct {
	// API is the operator-facing HTTP surface.
	API *api.Server
	// Agents is the QUIC control-stream server machines connect to.
	Agents *agentapi.AgentServer
	// AgentControl is the API server's view of Agents, bridged to its port.
	AgentControl api.AgentGetter
	// MPS is the TLS listener Intel AMT devices call in to.
	MPS *transport.Server
	// AMT is the management service driving those devices.
	AMT *amt.Service
	// Relay pairs an operator's session side with a machine's.
	Relay *relay.Relay
	// Signaling tracks WebRTC negotiation outcomes.
	Signaling *signaling.Tracker

	// Metrics and MetricsRegistry are this process's own instrumentation.
	Metrics         *appmetrics.Metrics
	MetricsRegistry *prometheus.Registry

	// Cert is the certificate authority machines are enrolled against.
	Cert *cert.Manager
	// JWT is the operator token configuration.
	JWT *auth.JWTConfig

	// Alerts holds incidents and the alert budget; Rules is the compiled pack.
	Alerts *alerts.Store
	Rules  *rules.Catalogue

	// Purger, PurgeJobs and Reconciler are the right-to-be-forgotten path.
	// All three are nil when numeric telemetry is off.
	Purger     api.DevicePurger
	PurgeJobs  api.PurgeJobReader
	Reconciler *lifecycle.Reconciler

	// SigningKeys and Manifests are the agent-update publishing path.
	SigningKeys *updater.SigningKeys
	Manifests   *updater.ManifestStore

	// The repositories a caller outside the HTTP surface needs — periodic
	// sweeps, and tests arranging a precondition the product offers no door for.
	Store          *db.PostgresStore
	Devices        device.Repository
	Sites          device.SiteRepository
	Organizations  organization.Repository
	Users          auth.UserRepository
	SecurityGroups auth.SecurityGroupRepository
	Sessions       session.Repository
	Enrollment     updater.EnrollmentTokenRepository
	Tombstones     *lifecycle.TombstoneStore

	// Logger is the logger every component above was given.
	Logger *slog.Logger
}

// validate refuses a configuration that would assemble a product with a hole
// in it. A missing port must fail here, naming itself, rather than becoming a
// route that answers 500 because chi's recoverer caught a nil dereference.
func (c Config) validate() error {
	switch {
	case c.Store == nil:
		return errors.New("app: Config.Store is required")
	case c.DataDir == "":
		return errors.New("app: Config.DataDir is required")
	case c.JWTSecret == "":
		return errors.New("app: Config.JWTSecret is required")
	case len(c.JWTSecret) < minJWTSecretLen:
		return fmt.Errorf("app: Config.JWTSecret must be at least %d characters", minJWTSecretLen)
	case c.Logger == nil:
		return errors.New("app: Config.Logger is required")
	}
	return nil
}

// Build assembles the whole product from cfg. ctx bounds the boot-time
// database work — resetting statuses left online by a previous run, warming
// the erasure deny-list, and resuming a purge a crash interrupted.
//
// It returns an error rather than exiting, so the same wiring serves the
// binary and the acceptance harness.
func Build(ctx context.Context, cfg Config) (*Assembly, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.DataDir, 0750); err != nil {
		return nil, fmt.Errorf("app: create data dir: %w", err)
	}

	logger := cfg.Logger
	store := cfg.Store

	metricsRegistry := appmetrics.NewRegistry()
	appMetrics := appmetrics.NewMetrics(metricsRegistry)

	repos := newRepositories(store.DB(), appMetrics)

	telemetryPorts := newTelemetryPorts(cfg, logger)
	tombstoneStore := lifecycle.NewTombstoneStore(store.DB())
	jobStore := lifecycle.NewJobStore(store.DB())

	// A status left online by a previous run is a lie the device list would
	// tell until the machine next connected, so it is cleared before serving.
	if err := repos.devices.ResetAllStatuses(dbtx.WithDefaultTenant(ctx, false)); err != nil {
		return nil, fmt.Errorf("app: reset device statuses: %w", err)
	}

	certMgr, err := cert.NewManager(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: init certificate manager: %w", err)
	}

	jwtCfg := &auth.JWTConfig{Secret: cfg.JWTSecret, Issuer: "opengate", Duration: jwtTokenLifetime}

	vapidPriv, vapidPub, err := notifications.LoadOrGenerateVAPID(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: init VAPID keys: %w", err)
	}
	notifier := notifications.NewPushNotifier(repos.webPush, vapidPriv, vapidPub, cfg.VAPIDContact, logger)

	agentRelay := relay.NewRelay(logger)
	agentRelay.OnSessionEnd = func(token protocol.SessionToken) {
		// A row the stale-session sweep already collected is the expected
		// race, not a failure — the session is gone either way.
		if err := cleanupRelaySession(repos.sessions, token); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
			logger.Error("cleanup session on disconnect", "error", err, "token_prefix", protocol.RedactToken(string(token)))
		}
	}

	// The rule catalogue is compiled in, so a failure here means the binary
	// itself is malformed — its contents are validated and cost-bounded in CI,
	// which is the whole reason definitions do not live in the database.
	ruleCatalogue, err := rules.Embedded()
	if err != nil {
		return nil, fmt.Errorf("app: load rule catalogue: %w", err)
	}
	ruleStore := rules.NewStore(store.DB())
	alertStore := alerts.NewStore(store.DB())

	// The shipped rule ids are the whole label vocabulary of the investigation
	// series, and the bound on it: rule ids travel to the agent and come back
	// on alerts and coverage reports, so without this the endpoint would decide
	// this server's cardinality. Seeding also exports every rule at zero, so a
	// rule that has raised nothing is distinguishable from a scrape that found
	// nothing.
	appMetrics.SeedRuleVocabulary(ruleIDs(ruleCatalogue))

	agentSrv := agentapi.NewAgentServer(agentapi.AgentServerConfig{
		Cert:          certMgr,
		Devices:       repos.devices,
		Hardware:      repos.hardware,
		DeviceUpdates: repos.deviceUpdates,
		Telemetry:     telemetryPorts.writer,
		Processes:     repos.processes,
		Inventory:     repos.inventory,
		Relay:         agentRelay,
		Notifier:      notifier,
		Metrics:       appMetrics,
		QuicHost:      cfg.QuicHost,
		Tombstones:    tombstoneStore,
		Settings:      settings.NewPostgresReader(store.DB()),
		// Each machine gets the curated pack as its customer has retuned it,
		// narrowed by the labels the machine carries, and the customer's
		// per-machine alert allowance travels down with it.
		AlertRules: agentapi.NewCatalogueAlertRuleProvider(
			ruleCatalogue, ruleStore, ruleStore, repos.devices, alertStore, logger),
		RuleCoverage: ruleStore,
		// The same store answers the fleet-wide fold the platform's own
		// coverage gauge is refreshed from.
		FleetCoverage: ruleStore,
		// An alert names a rule and carries a customer, so the store that files
		// it and the catalogue that says the rule exists are both wired here.
		AlertStore:    alertStore,
		RuleCatalogue: ruleCatalogue,
		Logger:        logger,
	})

	purger, purgeJobs, reconciler := buildPurgeOrchestrator(ctx, purgeDeps{
		agentSrv:        agentSrv,
		db:              store.DB(),
		tombstones:      tombstoneStore,
		jobs:            jobStore,
		seriesPurger:    telemetryPorts.purger,
		seriesInventory: telemetryPorts.inventory,
		investigations:  alertStore,
		logger:          logger,
	})

	signingKeys, err := updater.LoadOrGenerateSigningKeys(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: init update signing keys: %w", err)
	}
	manifestStore := updater.NewManifestStore(cfg.DataDir)

	mpsSrv := transport.NewServer(certMgr, repos.amt, repos.hardware, logger)
	amtSvc := amt.NewService(mpsSrv, cfg.AMTUser, cfg.AMTPass, logger)
	// Wired after construction: the service holds the MPS server, so the WSMAN
	// detail reader can only be handed back once both exist.
	mpsSrv.SetDetailProber(amtSvc)

	sigTracker := signaling.NewTracker(signaling.DefaultConfig())
	agentControl := agentControlGetter{srv: agentSrv}
	// One operator serves both the port the handlers hold and the port the
	// device page reads, so a stand-in cannot be wired into half the surface.
	management := amtOperator(cfg, amtSvc)

	srv := api.NewServer(api.ServerConfig{
		Store:                 store,
		Audit:                 repos.audit,
		AuditHandlers:         audit.NewHandlers(repos.audit),
		DeviceUpdates:         repos.deviceUpdates,
		Enrollment:            repos.enrollment,
		SecurityGroups:        repos.securityGroups,
		Devices:               repos.devices,
		Sites:                 repos.sites,
		Organizations:         repos.organizations,
		Hardware:              repos.hardware,
		Inventory:             repos.inventory,
		WebPush:               repos.webPush,
		NotificationsHandlers: notifications.NewHandlers(repos.webPush, notifier),
		AMTHandlers:           amt.NewHandlers(management),
		Sessions:              repos.sessions,
		Users:                 repos.users,
		JWT:                   jwtCfg,
		Agents:                agentControl,
		AMT:                   management,
		Cert:                  certMgr,
		TelemetryReader:       telemetryPorts.reader,
		Purger:                purger,
		PurgeJobs:             purgeJobs,
		Relay:                 agentRelay,
		Signaling:             sigTracker,
		Notifier:              notifier,
		Signing:               signingKeys,
		Manifests:             manifestStore,
		GitHubRepo:            cfg.GitHubRepo,
		BaseURL:               cfg.BaseURL,
		QuicHost:              cfg.QuicHost,
		Logger:                logger,
		WebDir:                cfg.WebDir,
		MetricsRegistry:       metricsRegistry,
		Metrics:               appMetrics,
		// The triage queue reads the same store the ingest path writes, and the
		// rules view is the compiled pack beside how far each rule has reached
		// and how much of an estate it is watching — the last read comes from
		// the connection server, which is the only thing that knows what is live.
		Investigations: alertStore,
		RuleCatalogue:  ruleCatalogue,
		RuleRollouts:   ruleStore,
		RuleCoverage:   agentSrv,
		// The same store holds everything an operator may change about a rule —
		// the tuned values, the pace it spreads at, the stop switch, and the
		// labels a rule is aimed at. The alert store holds the budget those
		// alerts are counted against, and the counts themselves.
		RuleAdmin:   ruleStore,
		AlertBudget: alertStore,
	})

	return &Assembly{
		API:             srv,
		Agents:          agentSrv,
		AgentControl:    agentControl,
		MPS:             mpsSrv,
		AMT:             amtSvc,
		Relay:           agentRelay,
		Signaling:       sigTracker,
		Metrics:         appMetrics,
		MetricsRegistry: metricsRegistry,
		Cert:            certMgr,
		JWT:             jwtCfg,
		Alerts:          alertStore,
		Rules:           ruleCatalogue,
		Purger:          purger,
		PurgeJobs:       purgeJobs,
		Reconciler:      reconciler,
		SigningKeys:     signingKeys,
		Manifests:       manifestStore,
		Store:           store,
		Devices:         repos.devices,
		Sites:           repos.sites,
		Organizations:   repos.organizations,
		Users:           repos.users,
		SecurityGroups:  repos.securityGroups,
		Sessions:        repos.sessions,
		Enrollment:      repos.enrollment,
		Tombstones:      tombstoneStore,
		Logger:          logger,
	}, nil
}

// amtOperator is the management service the API surface talks to: the one
// assembled over the MPS listener, unless the caller supplied a stand-in for
// hardware it cannot reach.
func amtOperator(cfg Config, svc *amt.Service) amt.Operator {
	if cfg.AMTOperator != nil {
		return cfg.AMTOperator
	}
	return svc
}

// repositories is every persistence adapter the product owns, wired against
// one connection pool and instrumented so they all land on the same
// db_query_* metrics.
type repositories struct {
	audit          audit.Repository
	deviceUpdates  updater.DeviceUpdateRepository
	enrollment     updater.EnrollmentTokenRepository
	securityGroups auth.SecurityGroupRepository
	devices        device.Repository
	sites          device.SiteRepository
	organizations  organization.Repository
	hardware       device.HardwareRepository
	webPush        notifications.WebPushRepository
	amt            amt.Repository
	sessions       session.Repository
	users          auth.UserRepository
	processes      telemetry.ProcessRepository
	inventory      inventory.Repository
}

func newRepositories(sqlDB *sql.DB, m *appmetrics.Metrics) repositories {
	return repositories{
		audit:          audit.NewInstrumented(audit.NewPostgres(sqlDB), m),
		deviceUpdates:  updater.NewInstrumentedDeviceUpdates(updater.NewPostgresDeviceUpdates(sqlDB), m),
		enrollment:     updater.NewInstrumentedEnrollment(updater.NewPostgresEnrollment(sqlDB), m),
		securityGroups: auth.NewInstrumentedSecurityGroups(auth.NewPostgresSecurityGroups(sqlDB), m),
		devices:        device.NewInstrumentedDevices(device.NewPostgresDevices(sqlDB), m),
		sites:          device.NewInstrumentedSites(device.NewPostgresSites(sqlDB), m),
		organizations:  organization.NewInstrumented(organization.NewPostgresOrganizations(sqlDB), m),
		hardware:       device.NewInstrumentedHardware(device.NewPostgresHardware(sqlDB), m),
		webPush:        notifications.NewInstrumentedWebPush(notifications.NewPostgresWebPush(sqlDB), m),
		amt:            amt.NewInstrumented(amt.NewPostgresAMTDevices(sqlDB), m),
		sessions:       session.NewInstrumented(session.NewPostgresSessions(sqlDB), m),
		users:          auth.NewInstrumentedUsers(auth.NewPostgresUsers(sqlDB), m),
		processes:      telemetry.NewPostgresProcessRepository(sqlDB),
		inventory:      inventory.NewPostgresInventoryRepository(sqlDB),
	}
}

// telemetryPorts are the four faces of the numeric metrics store: the writer
// the ingest path uses, the reader the device page reads, and the two erasure
// ports. All four are nil when no metrics store is configured.
type telemetryPorts struct {
	writer    telemetry.NumericWriter
	reader    api.MetricsReader
	purger    lifecycle.SeriesPurger
	inventory lifecycle.SubjectLister
}

func newTelemetryPorts(cfg Config, logger *slog.Logger) telemetryPorts {
	if cfg.VictoriaMetricsURL == "" {
		logger.Warn("edge sentinel numeric telemetry disabled: no metrics store configured")
		return telemetryPorts{}
	}
	client := telemetry.NewVMClient(cfg.VictoriaMetricsURL, nil)
	if cfg.VMDeleteAuthKey != "" {
		client = client.WithDeleteAuthKey(cfg.VMDeleteAuthKey)
	}
	logger.Info("edge sentinel telemetry writer enabled", "victoriametrics_url", cfg.VictoriaMetricsURL)
	return telemetryPorts{writer: client, reader: client, purger: client, inventory: client}
}

// agentControlGetter adapts *agentapi.AgentServer to api.AgentGetter. The
// server's getters return the concrete *agentapi.AgentConn while the api port
// speaks the api.AgentControl interface; Go has no covariant return types and
// agentapi cannot import api (that would cycle), so the composition root
// bridges the two here. A missing agent's typed-nil *AgentConn is converted to
// an interface nil so the handlers' `ac == nil` checks still fire.
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

// purgeDeps gathers the dependencies buildPurgeOrchestrator wires.
type purgeDeps struct {
	agentSrv        *agentapi.AgentServer
	db              *sql.DB
	tombstones      *lifecycle.TombstoneStore
	jobs            *lifecycle.JobStore
	seriesPurger    lifecycle.SeriesPurger
	seriesInventory lifecycle.SubjectLister
	// investigations repairs the incident bookkeeping a device erasure leaves
	// behind, which the foreign-key cascade cannot reach.
	investigations lifecycle.InvestigationPurger
	logger         *slog.Logger
}

// buildPurgeOrchestrator wires the right-to-be-forgotten purge orchestrator
// plus its reconciliation sweep. It needs a numeric metrics store to delete
// series, so it returns nils when that is absent and device deletion falls
// back to the plain Postgres delete. On success it warms the agent deny-list
// and resumes any purge a prior crash interrupted before the server serves.
func buildPurgeOrchestrator(ctx context.Context, d purgeDeps) (api.DevicePurger, api.PurgeJobReader, *lifecycle.Reconciler) {
	if d.seriesPurger == nil {
		return nil, nil, nil
	}
	orchestrator := lifecycle.NewOrchestrator(lifecycle.OrchestratorConfig{
		Tombstones: d.tombstones,
		Jobs:       d.jobs,
		Series:     d.seriesPurger,
		PG:         lifecycle.NewPostgresPurger(d.db, d.investigations),
		Edge:       d.agentSrv,
		Logger:     d.logger,
	})
	reconciler := lifecycle.NewReconciler(d.seriesInventory, d.seriesPurger,
		lifecycle.NewPostgresPurger(d.db, d.investigations), d.logger)

	startupCtx, cancel := context.WithTimeout(ctx, startupWorkBudget)
	defer cancel()
	if err := d.agentSrv.WarmTombstones(startupCtx); err != nil {
		d.logger.Error("warm tombstone deny-list", "error", err)
	}
	if err := orchestrator.Resume(startupCtx); err != nil {
		d.logger.Error("resume interrupted purges", "error", err)
	}
	return orchestrator, d.jobs, reconciler
}

// startupWorkBudget bounds the database work Build does before returning.
const startupWorkBudget = 60 * time.Second

// cleanupRelaySession removes the session row for a token the relay has
// finished with.
func cleanupRelaySession(repo session.Repository, token protocol.SessionToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), relayCleanupBudget)
	defer cancel()
	return repo.DeleteRelaySession(ctx, string(token))
}

// relayCleanupBudget bounds the single delete a finished session costs.
const relayCleanupBudget = 5 * time.Second

// ruleIDs is the label vocabulary the investigation series are bounded by: the
// ids of the rules this build actually ships, and nothing an endpoint can add
// to.
func ruleIDs(catalogue *rules.Catalogue) []string {
	shipped := catalogue.All()
	ids := make([]string, 0, len(shipped))
	for _, def := range shipped {
		ids = append(ids, def.ID)
	}
	return ids
}
