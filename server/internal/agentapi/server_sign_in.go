package agentapi

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// signIn waits the AgentRegister's turn in the line, then signs in under the status gate, and
// returns the slot to give back once its pushes are out; a full line returns errComeBackLater.
func (s *AgentServer) signIn(ctx context.Context, ac *AgentConn) (*protocol.ControlMessage, device.SignIn, func(), error) {
	msg, err := ac.awaitRegister(ctx)
	if err != nil {
		return nil, device.SignIn{}, nil, err
	}
	arrived := time.Now()
	leaveLine, err := s.line.enter(ctx, s.customerNote.customerOf(ac.DeviceID))
	if err != nil {
		return nil, device.SignIn{}, nil, err
	}
	leave := s.statusGate.enter(ac.DeviceID)
	defer leave()
	// The connection is held before the online status commits, so a machine the console shows
	// online is one a push reaches; the one it replaces is retired once the commit lands.
	var older *AgentConn
	signedIn, err := ac.signIn(ctx, msg, arrived, func(read device.SignIn) {
		ac.TenantID, ac.SiteID = read.TenantID, read.SiteID
		older = s.hold(ac)
	})
	if err != nil {
		s.letGo(ac, older)
		leaveLine()
		return nil, device.SignIn{}, nil, err
	}
	s.retireReplaced(ctx, older, ac)
	s.customerNote.remember(ac.DeviceID, signedIn.OrganizationID)
	return msg, signedIn, leaveLine, nil
}

// comeBackHangUpWait bounds how long a turned-away machine has to read its answer and hang up.
const comeBackHangUpWait = 5 * time.Second

// tellToComeBackLater answers a sign-in the line has no room for with a random wait, then closes
// the connection once the machine hangs up, which frees what the connection held.
func (s *AgentServer) tellToComeBackLater(ctx context.Context, ac *AgentConn, stream *quic.Stream,
	conn *quic.Conn, logger *slog.Logger,
) {
	if s.metrics != nil {
		s.metrics.SignInsTurnedAwayTotal.Inc()
	}
	wait := comeBackWait()
	if err := ac.SendComeBackLater(ctx, wait); err != nil {
		logger.Warn("tell a machine to come back later", "error", err)
	}
	_ = stream.Close()
	if !awaitHangUp(stream, conn, comeBackHangUpWait) {
		logger.Info("turned-away machine did not hang up")
	}
	closeWithCode(conn, protocol.CloseBye, "come back later")
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
