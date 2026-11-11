package agentapi

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// AgentConn represents an authenticated, connected agent.
type AgentConn struct {
	DeviceID protocol.DeviceID
	SiteID   uuid.UUID
	// TenantID is the authoritative tenant resolved by the server.
	TenantID uuid.UUID

	// metaMu guards the four registration fields below: the read loop writes them through setMeta,
	// and HTTP goroutines read them through Meta and requireCapability.
	metaMu       sync.RWMutex
	OS           string
	Arch         string
	AgentVersion string
	Capabilities []protocol.AgentCapability

	// isTombstoned reports whether this device has been purged, which drops its write-path messages
	// until the connection closes. A nil func means no deny-list is wired and reads as not purged.
	isTombstoned func() bool

	stream        io.ReadWriter
	codec         *protocol.Codec
	devices       device.Repository
	hardware      device.HardwareRepository
	deviceUpdates updater.DeviceUpdateRepository
	telemetry     ReadingWriter
	inventory     inventory.Repository
	scheduler     *BackfillScheduler
	alertRules    AlertRuleProvider
	alertStore    AlertRecorder
	ruleCatalog   *rules.Catalogue
	// wantedEventRules holds the event rules this customer still receives alerts from, read with the
	// connection's ruleset. A nil map admits every event rule.
	wantedEventRules map[string]struct{}
	// organizationID is the customer this machine belongs to, learned when its ruleset is resolved;
	// metaMu guards it with the rest of the snapshot.
	organizationID uuid.UUID
	coverage       *RuleCoverageStore
	ruleCoverage   UnsupportedCoverageStore
	settings       settings.Reader
	metrics        *appmetrics.Metrics
	logger         *slog.Logger
	telemetryLast  map[telemetryFloor]int64
	telemetrySlots chan struct{}
	telemetryDrops atomic.Uint64

	// maintenanceApplied is the last maintenance state the agent reported applying; Postgres holds
	// the desired state. The read loop writes it and any goroutine reads it.
	maintenanceApplied atomic.Bool

	// logMu guards logWaiter, the one in-flight raw-log pull, which is nil unless a pull awaits the
	// agent's DeviceLogsResponse. Raw logs are never persisted, so the connection scopes them.
	logMu     sync.Mutex
	logWaiter chan logsResult

	// historyMu guards historyWaiter, the one in-flight deep-history pull, since responses carry no
	// correlation id. historyWaiter is nil unless a pull awaits the agent's LocalHistoryResponse.
	historyMu     sync.Mutex
	historyWaiter chan historyResult

	// writeMu serializes writes to stream: WriteFrame writes a 5-byte envelope and then the payload,
	// and two unserialized senders would interleave them on one QUIC stream.
	writeMu sync.Mutex

	// released marks a connection the server has let go. A QUIC write lands in the local send buffer
	// with nobody reading the far end, so this flag is what refuses a send to a departed machine.
	released atomic.Bool

	// quicConn is the connection the stream rides, closed with a code to refuse or replace it.
	quicConn *quic.Conn
	// certFP and leaf are the certificate the connection presented, or the renewal it installed.
	certFP certledger.Fingerprint
	leaf   *x509.Certificate
	// authority is the trusted master key that signed leaf.
	authority cert.Authority
	// restricted marks a connection admitted only to renew or receive its removal order.
	restricted bool
	// renewal issues renewed certificates; renewalState is this connection's offer.
	renewal      *renewals
	renewalState renewalState
	// trustBundle is sent once registration completes, as the acknowledgement of the identity.
	trustBundle []byte

	// pongs carries the pongs the read loop records, which a liveness probe waits on.
	pongs chan struct{}

	// registered runs once the machine's row is written, with its capabilities known.
	registered func(*AgentConn)

	// allowance is the server's budget for messages read and not yet handled; nil budgets nothing.
	allowance *readAllowance
}

// answersPing sends a ping and reports whether the machine's pong arrives within wait; the read
// loop records the pong.
func (a *AgentConn) answersPing(wait time.Duration) bool {
	if a.pongs == nil || a.writeFrame(protocol.FramePing, nil) != nil {
		return false
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-a.pongs:
		return true
	case <-timer.C:
		return false
	}
}

// markReleased records that the server has let this connection go, so sendControl refuses from
// then on. Teardown calls it first, before the database writes that precede the close.
func (a *AgentConn) markReleased() { a.released.Store(true) }

// AgentMeta is a point-in-time snapshot of an agent's registration metadata, so the API layer reads
// the fields the update-eligibility filter needs without depending on *AgentConn.
type AgentMeta struct {
	DeviceID     protocol.DeviceID
	OS           string
	Arch         string
	AgentVersion string
	// OrganizationID and TenantID place this machine in the tenancy ladder, so an administrator's
	// change finds that customer's connected machines. OrganizationID is zero until a ruleset resolves.
	OrganizationID uuid.UUID
	TenantID       uuid.UUID
}

// Meta returns a snapshot of the agent's registration metadata, read under metaMu so a concurrent
// handleRegister cannot tear it.
func (a *AgentConn) Meta() AgentMeta {
	a.metaMu.RLock()
	defer a.metaMu.RUnlock()
	return AgentMeta{
		DeviceID:       a.DeviceID,
		OS:             a.OS,
		Arch:           a.Arch,
		AgentVersion:   a.AgentVersion,
		OrganizationID: a.organizationID,
		TenantID:       a.TenantID,
	}
}

// setMeta records the registration-reported OS, arch, version, and capabilities
// under the metadata guard. It is the single write path for those fields.
func (a *AgentConn) setMeta(osName, arch, version string, caps []protocol.AgentCapability) {
	a.metaMu.Lock()
	defer a.metaMu.Unlock()
	a.OS = osName
	a.Arch = arch
	a.AgentVersion = version
	a.Capabilities = caps
}

// AgentConnConfig bundles the dependencies an AgentConn needs.
type AgentConnConfig struct {
	DeviceID      protocol.DeviceID
	TenantID      uuid.UUID
	SiteID        uuid.UUID
	Stream        io.ReadWriter
	Devices       device.Repository
	Hardware      device.HardwareRepository
	DeviceUpdates updater.DeviceUpdateRepository
	Telemetry     ReadingWriter
	Inventory     inventory.Repository
	Scheduler     *BackfillScheduler
	AlertRules    AlertRuleProvider
	// Coverage records what this agent reports every rule is doing on it.
	// Optional: nil means coverage is not tracked for this connection.
	Coverage *RuleCoverageStore
	// RuleCoverage persists which rules this machine cannot evaluate.
	// Optional: nil keeps that state in memory only.
	RuleCoverage UnsupportedCoverageStore
	// Settings reads a machine's place in the tenancy ladder. Optional: nil
	// leaves the connection with the rungs it already knows for itself.
	Settings settings.Reader
	Metrics  *appmetrics.Metrics
	Logger   *slog.Logger
}

// NewAgentConn creates an AgentConn for testing or programmatic use.
func NewAgentConn(cfg AgentConnConfig) *AgentConn {
	return &AgentConn{
		DeviceID:      cfg.DeviceID,
		TenantID:      cfg.TenantID,
		SiteID:        cfg.SiteID,
		stream:        cfg.Stream,
		codec:         &protocol.Codec{},
		devices:       cfg.Devices,
		hardware:      cfg.Hardware,
		deviceUpdates: cfg.DeviceUpdates,
		telemetry:     cfg.Telemetry,
		inventory:     cfg.Inventory,
		scheduler:     cfg.Scheduler,
		alertRules:    cfg.AlertRules,
		coverage:      cfg.Coverage,
		ruleCoverage:  cfg.RuleCoverage,
		settings:      cfg.Settings,
		metrics:       cfg.Metrics,
		logger:        cfg.Logger,
	}
}

// sendControl encodes and writes a control message to the agent stream.
func (a *AgentConn) sendControl(msg *protocol.ControlMessage) error {
	if a.released.Load() {
		return fmt.Errorf("%w: %s not sent", ErrConnectionClosed, msg.Type)
	}
	payload, err := a.codec.EncodeControl(msg)
	if err != nil {
		return fmt.Errorf("encode %s: %w", msg.Type, err)
	}
	if err := a.writeFrame(protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write %s frame: %w", msg.Type, err)
	}
	return nil
}

func (a *AgentConn) requireCapability(required protocol.AgentCapability) error {
	// setMeta replaces the slice wholesale, so copying the header under the read lock and
	// iterating outside is race-safe.
	a.metaMu.RLock()
	caps := a.Capabilities
	a.metaMu.RUnlock()
	for _, advertised := range caps {
		if advertised == required {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrCapabilityNotAdvertised, required)
}

// writeFrame writes one framed message under writeMu, so concurrent senders cannot interleave
// envelope and payload bytes.
func (a *AgentConn) writeFrame(frameType byte, payload []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.codec.WriteFrame(a.stream, frameType, payload)
}

// Close closes the agent connection.
func (a *AgentConn) Close() error {
	if closer, ok := a.stream.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// handleUpdateAck logs how an agent update went and records it against the device.
func (a *AgentConn) handleUpdateAck(ctx context.Context, msg *protocol.ControlMessage) {
	status := updater.StatusSuccess
	if msg.Success != nil && *msg.Success {
		a.logger.Info("agent update applied", "device_id", a.DeviceID, "version", msg.Version)
	} else {
		status = updater.StatusFailed
		a.logger.Warn("agent update failed", "device_id", a.DeviceID, "version", msg.Version, "error", msg.AckError)
	}
	if err := a.deviceUpdates.SetStatus(ctx, a.DeviceID, msg.Version, status, msg.AckError); err != nil {
		a.logger.Warn("persist update ack failed", "device_id", a.DeviceID, "error", err)
	}
}

// handleControl reads and dispatches a single control message from the stream.
func (a *AgentConn) handleControl(ctx context.Context) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		ctx = dbtx.WithDefaultTenant(ctx, false)
	}

	frameType, payload, release, err := a.readFrame(ctx)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}
	defer release()

	if frameType == protocol.FramePing {
		return a.writeFrame(protocol.FramePong, nil)
	}
	if frameType == protocol.FramePong {
		select {
		case a.pongs <- struct{}{}:
		default:
		}
		return nil
	}

	if frameType != protocol.FrameControl {
		return fmt.Errorf("%w: expected control frame, got 0x%02x", ErrUnexpectedMessage, frameType)
	}

	msg, err := a.codec.DecodeControl(payload)
	if err != nil {
		return fmt.Errorf("decode control: %w", err)
	}

	if a.restricted {
		return a.dispatchRestricted(ctx, msg)
	}

	// A purged device is rejected on every write path, which closes the race between the purge
	// and this connection's teardown.
	if a.rejectTombstonedWrite(msg) {
		return nil
	}

	switch msg.Type {
	case protocol.MsgAgentRegister:
		return a.handleRegister(ctx, msg)
	case protocol.MsgAgentHeartbeat:
		return a.handleHeartbeat(ctx, msg)
	case protocol.MsgSessionAccept:
		a.logger.Info("session accepted", "device_id", a.DeviceID, "token_prefix", protocol.RedactToken(string(msg.Token)))
		return nil
	case protocol.MsgSessionReject:
		a.logger.Info("session rejected", "device_id", a.DeviceID, "token_prefix", protocol.RedactToken(string(msg.Token)), "reason", msg.Reason)
		return nil
	case protocol.MsgAgentUpdateAck:
		a.handleUpdateAck(ctx, msg)
		return nil
	case protocol.MsgHardwareReport:
		return a.handleHardwareReport(ctx, msg)
	case protocol.MsgHardwareReportError:
		a.logger.Warn("hardware report error from agent", "device_id", a.DeviceID, "error", msg.AckError)
		return nil
	case protocol.MsgDeviceLogsResponse:
		return a.handleDeviceLogsResponse(ctx, msg)
	case protocol.MsgDeviceLogsError:
		return a.handleDeviceLogsError(msg)
	case protocol.MsgAgentHealthSummary:
		return a.handleAgentHealthSummary(ctx, msg, len(payload))
	case protocol.MsgAgentMetricWindow:
		return a.handleAgentMetricWindow(ctx, msg, len(payload))
	case protocol.MsgDiscoveryReport:
		return a.handleDiscoveryReport(ctx, msg, len(payload))
	case protocol.MsgHealthWindowResponse:
		return a.handleHealthWindowResponse(ctx, msg, len(payload))
	case protocol.MsgRequestBackfillSlot:
		return a.handleRequestBackfillSlot(msg)
	case protocol.MsgMetricBackfillBatch:
		return a.handleMetricBackfillBatch(ctx, msg, len(payload))
	case protocol.MsgLocalHistoryResponse:
		return a.handleLocalHistoryResponse(msg)
	case protocol.MsgMaintenanceApplied:
		return a.handleMaintenanceApplied(msg)
	case protocol.MsgAgentAlert:
		return a.handleAgentAlert(ctx, msg, len(payload))
	case protocol.MsgCertificateSigningRequest:
		return a.handleCertificateRequest(ctx, msg)
	case protocol.MsgCertificateInstalled:
		return a.handleCertificateInstalled(ctx)
	default:
		a.logger.Debug("ignoring unknown control message", "device_id", a.DeviceID, "type", msg.Type)
		return nil
	}
}

// readFrame reads one frame inside the machine message ceiling and the server's read allowance,
// both applied to the declared length before the payload is allocated.
func (a *AgentConn) readFrame(ctx context.Context) (byte, []byte, func(), error) {
	frameType, length, err := a.codec.ReadFrameHeader(a.stream)
	if err != nil {
		return 0, nil, nil, err
	}
	if length > maxMachineMessageBytes {
		return 0, nil, nil, fmt.Errorf("%w: %d bytes (max %d)", protocol.ErrFrameTooLarge, length, maxMachineMessageBytes)
	}
	release, err := a.allowance.reserve(ctx, int64(length))
	if err != nil {
		return 0, nil, nil, err
	}
	payload, err := a.codec.ReadPayload(a.stream, length)
	if err != nil {
		release()
		return 0, nil, nil, err
	}
	return frameType, payload, release, nil
}

// rejectTombstonedWrite drops a write-path message from a purged device and
// reports that it was rejected, so the caller stops without persisting anything.
func (a *AgentConn) rejectTombstonedWrite(msg *protocol.ControlMessage) bool {
	if !isWritePathMessage(msg.Type) || a.isTombstoned == nil || !a.isTombstoned() {
		return false
	}
	a.dropTelemetry("tombstoned", "type", msg.Type)
	return true
}

// isWritePathMessage reports whether a control message creates or persists tenant data and so is
// denied for a tombstoned device.
func isWritePathMessage(t protocol.ControlMessageType) bool {
	switch t {
	case protocol.MsgAgentRegister,
		protocol.MsgAgentHeartbeat,
		protocol.MsgAgentHealthSummary,
		protocol.MsgAgentMetricWindow,
		protocol.MsgDiscoveryReport,
		protocol.MsgHealthWindowResponse,
		protocol.MsgRequestBackfillSlot,
		protocol.MsgMetricBackfillBatch,
		protocol.MsgAgentAlert:
		return true
	default:
		return false
	}
}

// DroppedTelemetryCount returns telemetry messages dropped by local bounds.
func (a *AgentConn) DroppedTelemetryCount() uint64 {
	return a.telemetryDrops.Load()
}

// IsCapabilityError reports whether err means an agent did not advertise the
// capability required for a server-to-agent control variant.
func IsCapabilityError(err error) bool {
	return errors.Is(err, ErrCapabilityNotAdvertised)
}

// handleHeartbeat writes nothing: presence is written on connect and disconnect, and the presence
// sweep refreshes last-seen.
func (a *AgentConn) handleHeartbeat(_ context.Context, msg *protocol.ControlMessage) error {
	a.logger.Debug("heartbeat",
		"device_id", a.DeviceID,
		"timestamp", msg.Timestamp,
	)

	return nil
}
