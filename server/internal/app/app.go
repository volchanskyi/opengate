// Package app is the composition root that wires every port to its adapter.
// It reads no flags or environment and opens no listener, so a test can assemble the product.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
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

// minJWTSecretLen is the shortest signing secret the product accepts, whatever assembles it.
const minJWTSecretLen = 32

// jwtTokenLifetime is how long an issued operator token stays valid.
const jwtTokenLifetime = 24 * time.Hour

// Config is the resolved configuration Build assembles from; every field is already worked out.
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

	// VictoriaMetricsURL enables numeric telemetry and series erasure; empty turns both off.
	VictoriaMetricsURL string
	// VMDeleteAuthKey authorises the delete API of that metrics store.
	VMDeleteAuthKey string
	// Namespace is the environment the server runs in, stamped onto every
	// reading it writes to that store. Production and staging share it.
	Namespace string

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
	// TrustedProxies lists the proxies whose X-Forwarded-For picks a caller's request allowance.
	// Empty trusts none, so callers behind one proxy share a single allowance.
	TrustedProxies string
	// WebDir holds the single-page application's static assets.
	WebDir string
	// InternalListen is the address the cluster-only listener binds. Empty
	// selects defaultInternalListen.
	InternalListen string

	// AMTOperator replaces the management service the API is given; nil uses the MPS-backed one.
	// Intel hardware answers on its own network path, so this is the one edge a harness stubs.
	AMTOperator amt.Operator
}

// Assembly is the wired product: every component that serves requests or runs periodic work.
type Assembly struct {
	// API is the operator-facing HTTP surface.
	API *api.Server
	// Internal is the listener only the cluster reaches: the Prometheus exposition and the profiler.
	Internal *http.Server
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
	// Signaling is the ICE configuration a browser is handed when it asks to upgrade a session.
	Signaling signaling.Config

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

	// githubRepo is the release feed agent manifests are synced from, read by the periodic sync.
	githubRepo string
}

// validate refuses a configuration with a hole in it, naming the missing port up front.
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

// Build assembles the whole product from cfg and returns any error to the caller.
// ctx bounds the boot-time database work and the server's lifetime; cancelling it ends relays.
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

	// A status left online by a previous run is cleared before serving.
	if err := repos.devices.ResetAllStatuses(dbtx.WithDefaultTenant(ctx, false)); err != nil {
		return nil, fmt.Errorf("app: reset device statuses: %w", err)
	}

	certMgr, err := cert.NewManager(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: init certificate manager: %w", err)
	}

	jwtCfg := &auth.JWTConfig{Secret: cfg.JWTSecret, Issuer: "opengate", Duration: jwtTokenLifetime}

	// A proxy name with a typo narrows the trusted set silently, so it is refused here.
	trustedProxies, err := api.ParseTrustedProxies(splitTrustedProxies(cfg.TrustedProxies))
	if err != nil {
		return nil, fmt.Errorf("app: read trusted proxies: %w", err)
	}

	vapidPriv, vapidPub, err := notifications.LoadOrGenerateVAPID(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("app: init VAPID keys: %w", err)
	}
	notifier := notifications.NewPushNotifier(repos.webPush, vapidPriv, vapidPub, cfg.VAPIDContact, logger)

	agentRelay := relay.NewRelay(logger)
	agentRelay.OnSessionEnd = func(token protocol.SessionToken) {
		// A row the stale-session sweep already collected is an expected race.
		if err := cleanupRelaySession(repos.sessions, token); err != nil && !errors.Is(err, session.ErrSessionNotFound) {
			logger.Error("cleanup session on disconnect", "error", err, "token_prefix", protocol.RedactToken(string(token)))
		}
	}

	// The rule catalogue is compiled in, so a failure here means the binary is malformed.
	ruleCatalogue, err := rules.Embedded()
	if err != nil {
		return nil, fmt.Errorf("app: load rule catalogue: %w", err)
	}
	ruleStore := rules.NewStore(store.DB())
	alertStore := alerts.NewStore(store.DB())

	// The shipped rule ids bound the investigation series' label cardinality.
	// Seeding exports every rule at zero, so a quiet rule differs from an empty scrape.
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
		// Each machine gets the pack as its customer retuned it, narrowed by its labels,
		// with the customer's per-machine alert allowance.
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

	sigConfig := signaling.DefaultConfig()
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
		Signaling:             sigConfig,
		Notifier:              notifier,
		Signing:               signingKeys,
		Manifests:             manifestStore,
		GitHubRepo:            cfg.GitHubRepo,
		BaseURL:               cfg.BaseURL,
		QuicHost:              cfg.QuicHost,
		TrustedProxies:        trustedProxies,
		Logger:                logger,
		WebDir:                cfg.WebDir,
		Metrics:               appMetrics,
		Lifetime:              ctx.Done(),
		// The triage queue reads the store ingest writes; live coverage comes from the connection server.
		Investigations: alertStore,
		RuleCatalogue:  ruleCatalogue,
		RuleRollouts:   ruleStore,
		RuleCoverage:   agentSrv,
		// The same store holds every operator change to a rule; the alert store holds budgets.
		RuleAdmin:   ruleStore,
		AlertBudget: alertStore,
	})

	// The exposition reads these four tallies when the page is built, so it shows live counts.
	if err := appMetrics.BindRuntimeCounts(appmetrics.GaugeSource{
		ActiveSessions:      agentRelay.ActiveSessionCount,
		SessionsStarted:     agentRelay.SessionsStarted,
		ConnectedAgents:     agentSrv.ConnectedAgentCount,
		ConnectedMPSDevices: amtSvc.ConnectedDeviceCount,
	}); err != nil {
		return nil, fmt.Errorf("app: publish the runtime counts: %w", err)
	}

	return &Assembly{
		API:             srv,
		Internal:        newInternalServer(cfg.InternalListen, metricsRegistry),
		Agents:          agentSrv,
		AgentControl:    agentControl,
		MPS:             mpsSrv,
		AMT:             amtSvc,
		Relay:           agentRelay,
		Signaling:       sigConfig,
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
		githubRepo:      cfg.GitHubRepo,
	}, nil
}

// amtOperator is the management service the API talks to: MPS-backed or the caller's stand-in.
func amtOperator(cfg Config, svc *amt.Service) amt.Operator {
	if cfg.AMTOperator != nil {
		return cfg.AMTOperator
	}
	return svc
}

// repositories is every persistence adapter, sharing one pool and the db_query_* metrics.
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

// telemetryPorts are the writer, reader and two erasure ports; all are nil without a metrics store.
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
	if cfg.Namespace != "" {
		client = client.WithNamespace(cfg.Namespace)
	}
	logger.Info("edge sentinel telemetry writer enabled", "victoriametrics_url", cfg.VictoriaMetricsURL)
	return telemetryPorts{writer: client, reader: client, purger: client, inventory: client}
}

// agentControlGetter adapts *agentapi.AgentServer to api.AgentGetter.
// A missing agent's typed-nil *AgentConn becomes an interface nil so `ac == nil` checks fire.
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

func (g agentControlGetter) RefreshAlertRules(ctx context.Context, organizationID uuid.UUID) int {
	return g.srv.RefreshAlertRules(ctx, organizationID)
}

func (g agentControlGetter) RefreshAlertRulesForTenant(ctx context.Context, tenantID uuid.UUID) int {
	return g.srv.RefreshAlertRulesForTenant(ctx, tenantID)
}

func (g agentControlGetter) ListConnectedAgents() []api.AgentControl {
	conns := g.srv.ListConnectedAgents()
	out := make([]api.AgentControl, 0, len(conns))
	for _, ac := range conns {
		out = append(out, ac)
	}
	return out
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

// buildPurgeOrchestrator wires the purge orchestrator and its reconciliation sweep.
// Without a metrics store it returns nils and the server refuses device deletes.
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

// ruleIDs is the label vocabulary of the investigation series: the ids of the shipped rules.
func ruleIDs(catalogue *rules.Catalogue) []string {
	shipped := catalogue.All()
	ids := make([]string, 0, len(shipped))
	for _, def := range shipped {
		ids = append(ids, def.ID)
	}
	return ids
}

// splitTrustedProxies splits the configured proxies on commas, whitespace and newlines.
func splitTrustedProxies(configured string) []string {
	return strings.FieldsFunc(configured, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}
