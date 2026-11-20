package agentapi

import (
	"context"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// signIn reads the machine's AgentRegister, then signs it in and holds it under one status gate,
// so a departing older connection goes offline first or finds this one in its place.
func (s *AgentServer) signIn(ctx context.Context, ac *AgentConn) (*protocol.ControlMessage, device.SignIn, error) {
	msg, err := ac.awaitRegister(ctx)
	if err != nil {
		return nil, device.SignIn{}, err
	}
	leave := s.statusGate.enter(ac.DeviceID)
	defer leave()
	signedIn, err := ac.signIn(ctx, msg)
	if err != nil {
		return nil, device.SignIn{}, err
	}
	ac.TenantID, ac.SiteID = signedIn.TenantID, signedIn.SiteID
	s.swapIn(ctx, ac)
	return msg, signedIn, nil
}

// awaitRegister reads the machine's first control message, answering pings on the way; the agent
// sends its AgentRegister first on every connection, so anything else ends the connection.
func (a *AgentConn) awaitRegister(ctx context.Context) (*protocol.ControlMessage, error) {
	for {
		frameType, payload, release, err := a.readFrame(ctx)
		if err != nil {
			return nil, fmt.Errorf("read frame: %w", err)
		}
		if frameType == protocol.FramePing {
			release()
			if err := a.writeFrame(protocol.FramePong, nil); err != nil {
				return nil, err
			}
			continue
		}
		if frameType != protocol.FrameControl {
			release()
			return nil, fmt.Errorf("%w: the sign-in first, got frame 0x%02x", ErrUnexpectedMessage, frameType)
		}
		msg, err := a.codec.DecodeControl(payload)
		release()
		if err != nil {
			return nil, fmt.Errorf("decode control: %w", err)
		}
		if msg.Type != protocol.MsgAgentRegister {
			return nil, fmt.Errorf("%w: the sign-in first, got message %v", ErrUnexpectedMessage, msg.Type)
		}
		return msg, nil
	}
}
