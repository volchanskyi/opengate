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
	fleetCoverage  InstallCounter
	settings       settings.Reader
	metrics        *appmetrics.Metrics
	quicHost       string   // extra DNS SAN for the server certificate
	conns          sync.Map // map[protocol.DeviceID]*AgentConn
	statusGate     deviceStatusGate
	count          atomic.Int64
	tombstones     sync.Map // map[protocol.DeviceID]struct{} — deleted devices (in-memory deny-list)
	tombstoneStore tombstoneLoader
	logger         *slog.Logger
	addrCh         chan string // signals the actual listen address
	addrOnce       sync.Once
}

// AgentServerConfig gathers the AgentServer constructor's dependencies.
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
	// AlertRules provides each agent's threshold-alert ruleset for its place in the
	// tenancy ladder. Optional; nil falls back to DefaultAlertRules for every tenant.
	AlertRules AlertRuleProvider
	// RuleCoverage persists which machines cannot evaluate a rule at all.
	// Optional; nil keeps coverage in memory.
	RuleCoverage UnsupportedCoverageStore
	// FleetCoverage counts the fleet size and, per rule, the machines that cannot evaluate it.
	// Optional; nil leaves fleet-wide coverage unreported.
	FleetCoverage InstallCounter
	// Settings reads a machine's place in the tenancy ladder so alerts and vitals carry
	// the right customer. Optional; nil leaves each connection with the rungs it knows.
	Settings settings.Reader
	// Tombstones is the persisted deny-list that warms the in-memory cache at startup.
	// Optional; nil disables warming, and live purges still update the cache.
	Tombstones tombstoneLoader
	// AlertStore files alerts from connected agents. Optional; nil counts every alert
	// as a typed drop, so an unstored alert never reads as a stored one.
	AlertStore AlertRecorder
	// RuleCatalogue lists the rules this build ships; an alert naming another is refused.
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

// addrWait bounds how long Addr waits for the listener to bind: wide enough for a slow
// machine, short enough that a listener that never comes up is reported.
const addrWait = 30 * time.Second

// Addr waits for the listener and returns its address, or the empty string when it never
// bound, so a dial fails immediately and never waits on a channel nobody writes.
func (s *AgentServer) Addr() string {
	addr, listening := waitForAddr(s.addrCh, addrWait)
	if !listening {
		s.logger.Error("the agent listener did not bind", "waited", addrWait)
	}
	return addr
}

// waitForAddr reads the address the listener published, and reports whether one
// arrived before the wait ran out.
func waitForAddr(ch <-chan string, wait time.Duration) (string, bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case addr := <-ch:
		return addr, true
	case <-timer.C:
		return "", false
	}
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
