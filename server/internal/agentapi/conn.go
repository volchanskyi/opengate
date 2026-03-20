package agentapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// AgentConn represents an authenticated, connected agent.
type AgentConn struct {
	// DeviceID is the agent's unique device identifier.
	DeviceID protocol.DeviceID
	// GroupID is the group this agent belongs to (set during registration).
	GroupID uuid.UUID
	// OS reported by the agent during registration.
	OS string
	// Arch reported by the agent during registration.
	Arch string
	// AgentVersion reported by the agent during registration.
	AgentVersion string
	// Capabilities reported by the agent during registration.
	Capabilities []protocol.AgentCapability

	stream io.ReadWriter
	codec  *protocol.Codec
	store  db.Store
	logger *slog.Logger
}

// NewAgentConn creates an AgentConn for testing or programmatic use.
func NewAgentConn(deviceID protocol.DeviceID, groupID uuid.UUID, stream io.ReadWriter, store db.Store, logger *slog.Logger) *AgentConn {
	return &AgentConn{
		DeviceID: deviceID,
		GroupID:  groupID,
		stream:   stream,
		codec:    &protocol.Codec{},
		store:    store,
		logger:   logger,
	}
}

// sendControl encodes and writes a control message to the agent stream.
func (a *AgentConn) sendControl(msg *protocol.ControlMessage) error {
	payload, err := a.codec.EncodeControl(msg)
	if err != nil {
		return fmt.Errorf("encode %s: %w", msg.Type, err)
	}
	if err := a.codec.WriteFrame(a.stream, protocol.FrameControl, payload); err != nil {
		return fmt.Errorf("write %s frame: %w", msg.Type, err)
	}
	return nil
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

// Close closes the agent connection.
func (a *AgentConn) Close() error {
	if closer, ok := a.stream.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// handleControl reads and dispatches a single control message from the stream.
func (a *AgentConn) handleControl(ctx context.Context) error {
	frameType, payload, err := a.codec.ReadFrame(a.stream)
	if err != nil {
		return fmt.Errorf("read frame: %w", err)
	}

	if frameType == protocol.FramePing {
		return a.codec.WriteFrame(a.stream, protocol.FramePong, nil)
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
		return nil
	default:
		return fmt.Errorf("%w: %s", ErrUnexpectedMessage, msg.Type)
	}
}

func (a *AgentConn) handleRegister(ctx context.Context, msg *protocol.ControlMessage) error {
	a.Capabilities = msg.Capabilities
	a.OS = msg.OS
	a.Arch = msg.Arch
	a.AgentVersion = msg.Version

	device := &db.Device{
		ID:           a.DeviceID,
		GroupID:      a.GroupID,
		Hostname:     msg.Hostname,
		OS:           msg.OS,
		AgentVersion: msg.Version,
		Status:       db.StatusOnline,
	}

	if err := a.store.UpsertDevice(ctx, device); err != nil {
		return fmt.Errorf("upsert device: %w", err)
	}

	if err := a.store.SetDeviceStatus(ctx, a.DeviceID, db.StatusOnline); err != nil {
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
	if err := a.store.SetDeviceStatus(ctx, a.DeviceID, db.StatusOnline); err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}

	a.logger.Debug("heartbeat",
		"device_id", a.DeviceID,
		"timestamp", msg.Timestamp,
	)

	return nil
}

