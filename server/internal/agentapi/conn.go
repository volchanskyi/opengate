package agentapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/osutil"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// AgentConn represents an authenticated, connected agent.
type AgentConn struct {
	// DeviceID is the agent's unique device identifier.
	DeviceID protocol.DeviceID
	// GroupID is the group this agent belongs to (set during registration).
	GroupID uuid.UUID
	// OrgID is the authoritative organization resolved by the server.
	OrgID uuid.UUID
	// OS reported by the agent during registration.
	OS string
	// Arch reported by the agent during registration.
	Arch string
	// AgentVersion reported by the agent during registration.
	AgentVersion string
	// Capabilities reported by the agent during registration.
	Capabilities []protocol.AgentCapability

	stream         io.ReadWriter
	codec          *protocol.Codec
	devices        device.Repository
	hardware       device.HardwareRepository
	deviceUpdates  updater.DeviceUpdateRepository
	telemetry      telemetry.NumericWriter
	processes      telemetry.ProcessRepository
	logger         *slog.Logger
	telemetryLast  map[protocol.ControlMessageType]int64
	telemetrySlots chan struct{}
	telemetryDrops atomic.Uint64

	// logMu guards logWaiter, the single in-flight raw-log broker channel.
	// Raw log retrieval is transient (request→response) and never persisted,
	// so isolation is the connection scope; logWaiter is nil unless a pull is
	// blocked awaiting the agent's DeviceLogsResponse.
	logMu     sync.Mutex
	logWaiter chan logsResult

	// writeMu serializes writes to stream. protocol.Codec.WriteFrame issues
	// a 5-byte envelope write followed by an N-byte payload write; without
	// this mutex two concurrent outbound sendControl calls could
	// interleave their (header, payload) pairs on the same QUIC stream and
	// corrupt the frame seen by the agent.
	writeMu sync.Mutex
}

// AgentConnConfig bundles the dependencies an AgentConn needs. Promoted
// from a positional argument list when the latter exceeded Sonar's
// parameter cap while the shared Store dependency was split into narrow ports.
type AgentConnConfig struct {
	DeviceID      protocol.DeviceID
	OrgID         uuid.UUID
	GroupID       uuid.UUID
	Stream        io.ReadWriter
	Devices       device.Repository
	Hardware      device.HardwareRepository
	DeviceUpdates updater.DeviceUpdateRepository
	Telemetry     telemetry.NumericWriter
	Processes     telemetry.ProcessRepository
	Logger        *slog.Logger
}

// NewAgentConn creates an AgentConn for testing or programmatic use.
func NewAgentConn(cfg AgentConnConfig) *AgentConn {
	return &AgentConn{
		DeviceID:      cfg.DeviceID,
		OrgID:         cfg.OrgID,
		GroupID:       cfg.GroupID,
		stream:        cfg.Stream,
		codec:         &protocol.Codec{},
		devices:       cfg.Devices,
		hardware:      cfg.Hardware,
		deviceUpdates: cfg.DeviceUpdates,
		telemetry:     cfg.Telemetry,
		processes:     cfg.Processes,
		logger:        cfg.Logger,
	}
}

// sendControl encodes and writes a control message to the agent stream.
func (a *AgentConn) sendControl(msg *protocol.ControlMessage) error {
	payload, err := a.codec.EncodeControl(msg)
	if err != nil {
		return fmt.Errorf("encode %s: %w", msg.Type, err)
	}
	if err := a.writeFrame(protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write %s frame: %w", msg.Type, err)
	}
	return nil
}

func (a *AgentConn) requireCapability(cap protocol.AgentCapability) error {
	for _, advertised := range a.Capabilities {
		if advertised == cap {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrCapabilityNotAdvertised, cap)
}

// writeFrame writes a single framed message to the agent stream while
// holding writeMu so concurrent writers (API-handler-initiated sendControl
// plus the read-loop's FramePong response) cannot interleave envelope and
// payload bytes.
func (a *AgentConn) writeFrame(frameType byte, payload []byte) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return a.codec.WriteFrame(a.stream, frameType, payload)
}

// SendSessionRequest sends a SessionRequest control message to the agent.
func (a *AgentConn) SendSessionRequest(ctx context.Context, token protocol.SessionToken, relayURL string, perms protocol.Permissions) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:        protocol.MsgSessionRequest,
		Token:       token,
		RelayURL:    relayURL,
		Permissions: &perms,
	})
}

// SendAgentUpdate sends an AgentUpdate control message to the agent.
func (a *AgentConn) SendAgentUpdate(ctx context.Context, version, url, sha256, signature string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:      protocol.MsgAgentUpdate,
		Version:   version,
		URL:       url,
		SHA256:    sha256,
		Signature: signature,
	})
}

// SendAgentDeregistered tells the agent its device was deleted and it should clean up.
func (a *AgentConn) SendAgentDeregistered(ctx context.Context, reason string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgAgentDeregistered,
		Reason: reason,
	})
}

// SendRestartAgent asks the agent to restart itself.
func (a *AgentConn) SendRestartAgent(ctx context.Context, reason string) error {
	return a.sendControl(&protocol.ControlMessage{
		Type:   protocol.MsgRestartAgent,
		Reason: reason,
	})
}

// SendRequestHardwareReport asks the agent to collect and send hardware info.
func (a *AgentConn) SendRequestHardwareReport(ctx context.Context) error {
	if err := a.requireCapability(protocol.CapHardwareInventory); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type: protocol.MsgRequestHardwareReport,
	})
}

// SendRequestHealthWindow asks the agent for its bounded recent health summary window.
func (a *AgentConn) SendRequestHealthWindow(ctx context.Context, sinceTS int64, limit uint32) error {
	if err := a.requireCapability(protocol.CapHealthWindow); err != nil {
		return err
	}
	return a.sendControl(&protocol.ControlMessage{
		Type:    protocol.MsgRequestHealthWindow,
		SinceTS: sinceTS,
		Limit:   limit,
	})
}

// SendRequestDeviceLogs asks the agent to collect and send filtered log entries.
func (a *AgentConn) SendRequestDeviceLogs(ctx context.Context, filter device.LogFilter) error {
	if err := a.requireCapability(protocol.CapDeviceLogs); err != nil {
		return err
	}
	offset := clampNonNegativeUint32(filter.Offset)
	limit := clampNonNegativeUint32(filter.Limit)
	return a.sendControl(&protocol.ControlMessage{
		Type:      protocol.MsgRequestDeviceLogs,
		LogLevel:  filter.Level,
		TimeFrom:  filter.From,
		TimeTo:    filter.To,
		Search:    filter.Search,
		LogOffset: offset,
		LogLimit:  limit,
	})
}

// clampNonNegativeUint32 narrows a non-negative int to uint32, clamping any
// value outside [0, math.MaxUint32] to the boundary. Negative values become 0.
func clampNonNegativeUint32(v int) uint32 {
	if v <= 0 {
		return 0
	}
	if uint64(v) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

// clampInt64 narrows uint64 to int64, capping at math.MaxInt64 to avoid sign flip.
func clampInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// Close closes the agent connection.
func (a *AgentConn) Close() error {
	if closer, ok := a.stream.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// handleControl reads and dispatches a single control message from the stream.
func (a *AgentConn) handleControl(ctx context.Context) error {
	if _, ok := dbtx.TenantFromContext(ctx); !ok {
		ctx = dbtx.WithDefaultTenant(ctx, false)
	}

	frameType, payload, err := a.codec.ReadFrame(a.stream)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}

	if frameType == protocol.FramePing {
		return a.writeFrame(protocol.FramePong, nil)
	}

	if frameType != protocol.FrameControl {
		return fmt.Errorf("%w: expected control frame, got 0x%02x", ErrUnexpectedMessage, frameType)
	}

	msg, err := a.codec.DecodeControl(payload)
	if err != nil {
		return fmt.Errorf("decode control: %w", err)
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
		success := msg.Success != nil && *msg.Success
		if success {
			a.logger.Info("agent update applied", "device_id", a.DeviceID, "version", msg.Version)
		} else {
			a.logger.Warn("agent update failed", "device_id", a.DeviceID, "version", msg.Version, "error", msg.AckError)
		}

		// Persist update outcome.
		status := updater.StatusSuccess
		if !success {
			status = updater.StatusFailed
		}
		if err := a.deviceUpdates.SetStatus(ctx, a.DeviceID, msg.Version, status, msg.AckError); err != nil {
			a.logger.Warn("persist update ack failed", "device_id", a.DeviceID, "error", err)
		}
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
	case protocol.MsgProcessReport:
		return a.handleProcessReport(ctx, msg, len(payload))
	case protocol.MsgHealthWindowResponse:
		return a.handleHealthWindowResponse(ctx, msg, len(payload))
	default:
		a.logger.Debug("ignoring unknown control message", "device_id", a.DeviceID, "type", msg.Type)
		return nil
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

func (a *AgentConn) handleHardwareReport(ctx context.Context, msg *protocol.ControlMessage) error {
	nis := make([]device.NetworkInterfaceInfo, len(msg.NetworkInterfaces))
	for i, ni := range msg.NetworkInterfaces {
		nis[i] = device.NetworkInterfaceInfo{
			Name: ni.Name,
			MAC:  ni.MAC,
			IPv4: ni.IPv4,
			IPv6: ni.IPv6,
		}
	}

	hw := &device.Hardware{
		DeviceID:          a.DeviceID,
		CPUModel:          msg.CPUModel,
		CPUCores:          int(msg.CPUCores), // uint32 -> int: always fits on supported (64-bit) platforms.
		RAMTotalMB:        clampInt64(msg.RAMTotalMB),
		DiskTotalMB:       clampInt64(msg.DiskTotalMB),
		DiskFreeMB:        clampInt64(msg.DiskFreeMB),
		NetworkInterfaces: nis,
	}
	if err := a.hardware.Upsert(ctx, hw); err != nil {
		return fmt.Errorf("upsert hardware: %w", err)
	}

	a.logger.Debug("hardware report stored", "device_id", a.DeviceID)
	return nil
}

func (a *AgentConn) handleRegister(ctx context.Context, msg *protocol.ControlMessage) error {
	a.Capabilities = msg.Capabilities
	a.OS = osutil.NormalizeOS(msg.OS)
	a.Arch = osutil.NormalizeArch(msg.Arch)
	a.AgentVersion = msg.Version

	caps := make([]string, len(msg.Capabilities))
	for i, c := range msg.Capabilities {
		caps[i] = string(c)
	}

	d := &device.Device{
		ID:           a.DeviceID,
		GroupID:      a.GroupID,
		Hostname:     msg.Hostname,
		OS:           a.OS,
		OsDisplay:    msg.OS,
		AgentVersion: msg.Version,
		Capabilities: caps,
		Status:       device.StatusOnline,
	}

	if err := a.devices.Upsert(ctx, d); err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}

	if err := a.devices.SetStatus(ctx, a.DeviceID, device.StatusOnline); err != nil {
		return fmt.Errorf("set device online: %w", err)
	}

	a.logger.Info("agent registered",
		"device_id", a.DeviceID,
		"hostname", msg.Hostname,
		"os", msg.OS,
		"capabilities", msg.Capabilities,
	)

	return nil
}

func (a *AgentConn) handleHeartbeat(ctx context.Context, msg *protocol.ControlMessage) error {
	if err := a.devices.SetStatus(ctx, a.DeviceID, device.StatusOnline); err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}

	a.logger.Debug("heartbeat",
		"device_id", a.DeviceID,
		"timestamp", msg.Timestamp,
	)

	return nil
}
