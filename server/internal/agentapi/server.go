package agentapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// AgentServer accepts QUIC connections from agents and manages their lifecycle.
type AgentServer struct {
	cert           *cert.Manager
	devices        device.Repository
	hardware       device.HardwareRepository
	deviceUpdates  updater.DeviceUpdateRepository
	telemetry      telemetry.NumericWriter
	processes      telemetry.ProcessRepository
	inventory      inventory.Repository
	relay          *relay.Relay
	notifier       notifications.Notifier
	scheduler      *BackfillScheduler
	alertRules     AlertRuleProvider
	alertStore     AlertRecorder
	ruleCatalog    *rules.Catalogue
	coverage       *RuleCoverageStore
	ruleCoverage   UnsupportedCoverageStore
	fleetCoverage  FleetCoverageSource
	settings       settings.Reader
	metrics        *appmetrics.Metrics
	quicHost       string   // extra DNS SAN for the server certificate
	conns          sync.Map // map[protocol.DeviceID]*AgentConn
	count          atomic.Int64
	tombstones     sync.Map // map[protocol.DeviceID]struct{} — deleted devices (in-memory deny-list)
	tombstoneStore tombstoneLoader
	logger         *slog.Logger
	addrCh         chan string // signals the actual listen address
	addrOnce       sync.Once
}

// AgentServerConfig gathers the AgentServer constructor's dependencies. A
// struct rather than a long parameter list keeps the call sites readable now
// that persistence ports are split across their consuming modules.
type AgentServerConfig struct {
	Cert          *cert.Manager
	Devices       device.Repository
	Hardware      device.HardwareRepository
	DeviceUpdates updater.DeviceUpdateRepository
	Telemetry     telemetry.NumericWriter
	Processes     telemetry.ProcessRepository
	Inventory     inventory.Repository
	Relay         *relay.Relay
	Notifier      notifications.Notifier
	Metrics       *appmetrics.Metrics
	QuicHost      string
	Logger        *slog.Logger
	// AlertRules provides each connecting agent's threshold-alert ruleset,
	// resolved against the machine's place in the tenancy ladder. Optional: nil
	// falls back to DefaultAlertRules for every tenant.
	AlertRules AlertRuleProvider
	// RuleCoverage persists the one coverage state that is durable: which
	// machines cannot evaluate a rule at all. Optional; nil keeps coverage
	// entirely in memory.
	RuleCoverage UnsupportedCoverageStore
	// FleetCoverage counts the whole install for the aggregate coverage gauge:
	// the fleet size, and per rule how many machines cannot evaluate it.
	// Optional; nil leaves the platform's fleet-wide coverage view unreported.
	FleetCoverage FleetCoverageSource
	// Settings reads a machine's place in the tenancy ladder, so alerts and
	// vitals arriving on an agent connection carry the right customer. Optional:
	// nil leaves each connection with the rungs it already knows for itself.
	Settings settings.Reader
	// Tombstones is the persisted deny-list used to warm the in-memory cache at
	// startup so a purged device stays rejected across restarts. Optional: nil
	// disables warming (live purges still update the in-memory cache).
	Tombstones tombstoneLoader
	// AlertStore files the alerts arriving from connected agents. Optional; nil
	// counts every alert as a typed drop rather than pretending it landed —
	// there is no path for asking the endpoint again, so an unstored alert must
	// never read as a stored one.
	AlertStore AlertRecorder
	// RuleCatalogue says which rules this build ships, so an alert naming one it
	// does not is refused rather than stored as a row nobody can act on.
	// Optional; nil accepts any rule id.
	RuleCatalogue *rules.Catalogue
}

// NewAgentServer creates a new AgentServer.
func NewAgentServer(cfg AgentServerConfig) *AgentServer {
	return &AgentServer{
		cert:           cfg.Cert,
		devices:        cfg.Devices,
		hardware:       cfg.Hardware,
		deviceUpdates:  cfg.DeviceUpdates,
		telemetry:      cfg.Telemetry,
		processes:      cfg.Processes,
		inventory:      cfg.Inventory,
		relay:          cfg.Relay,
		notifier:       cfg.Notifier,
		scheduler:      NewBackfillScheduler(DefaultBackfillSchedulerConfig(), nil, nil),
		alertRules:     resolveAlertRuleProvider(cfg.AlertRules),
		alertStore:     cfg.AlertStore,
		ruleCatalog:    cfg.RuleCatalogue,
		coverage:       NewRuleCoverageStore(),
		ruleCoverage:   cfg.RuleCoverage,
		fleetCoverage:  cfg.FleetCoverage,
		settings:       cfg.Settings,
		metrics:        cfg.Metrics,
		quicHost:       cfg.QuicHost,
		tombstoneStore: cfg.Tombstones,
		logger:         cfg.Logger,
		addrCh:         make(chan string, 1),
	}
}

// ConnectedAgentCount returns the number of currently connected agents.
func (s *AgentServer) ConnectedAgentCount() int {
	return int(s.count.Load())
}

// GetAgent returns the AgentConn for the given device, or nil if not connected.
func (s *AgentServer) GetAgent(deviceID protocol.DeviceID) *AgentConn {
	val, ok := s.conns.Load(deviceID)
	if !ok {
		return nil
	}
	return val.(*AgentConn)
}

// ListConnectedAgents returns all currently connected agents.
func (s *AgentServer) ListConnectedAgents() []*AgentConn {
	var agents []*AgentConn
	s.conns.Range(func(_, value any) bool {
		agents = append(agents, value.(*AgentConn))
		return true
	})
	return agents
}

// Addr blocks until the server is listening and returns the actual address.
func (s *AgentServer) Addr() string {
	return <-s.addrCh
}

// ListenAndServe starts the QUIC listener and blocks until ctx is cancelled.
func (s *AgentServer) ListenAndServe(ctx context.Context, addr string) error {
	var extraDNS []string
	if s.quicHost != "" {
		extraDNS = append(extraDNS, s.quicHost)
	}
	tlsCfg, err := s.cert.ServerTLSConfig(extraDNS...)
	if err != nil {
		return fmt.Errorf("server TLS config: %w", err)
	}

	quicCfg := &quic.Config{
		MaxIdleTimeout:  90 * time.Second,
		KeepAlivePeriod: 30 * time.Second,
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve addr: %w", err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen udp: %w", err)
	}

	tr := &quic.Transport{Conn: udpConn}
	defer tr.Close()

	listener, err := tr.Listen(tlsCfg, quicCfg)
	if err != nil {
		return fmt.Errorf("quic listen: %w", err)
	}
	defer listener.Close()

	actualAddr := listener.Addr().String()
	s.addrOnce.Do(func() {
		s.addrCh <- actualAddr
		close(s.addrCh)
	})

	s.logger.Info("agent QUIC server listening", "addr", actualAddr)

	// Accept connections until context is cancelled
	for {
		conn, err := listener.Accept(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				return nil
			}
			s.logger.Error("accept error", "error", err)
			continue
		}

		go s.accept(ctx, conn)
	}
}
