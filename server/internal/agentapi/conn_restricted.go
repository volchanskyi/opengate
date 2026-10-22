package agentapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// restrictedSessionLimit is how long a connection admitted only to renew stays open.
const restrictedSessionLimit = 45 * time.Second

// serveRestricted serves a machine admitted only to renew: a connection object with no
// repositories, never stored, which closes with code 4 after its renewal or its time.
func (s *AgentServer) serveRestricted(ctx context.Context, stream *quic.Stream, conn *quic.Conn, a admission, logger *slog.Logger) {
	ac := &AgentConn{
		DeviceID:   a.class.DeviceID,
		stream:     stream,
		codec:      &protocol.Codec{},
		logger:     logger,
		quicConn:   conn,
		restricted: true,
		renewal:    s.renewals,
		certFP:     a.fp,
		leaf:       a.class.Leaf,
		authority:  a.class.Authority,
	}
	s.restricted.Store(ac, struct{}{})
	defer s.restricted.Delete(ac)
	logger.Info("admitted only to renew", "standing", a.class.Standing, "expired", a.class.Expired)

	deadline, cancel := context.WithTimeout(ctx, restrictedSessionLimit)
	defer cancel()
	go func() {
		<-deadline.Done()
		closeWithCode(conn, protocol.CloseRestrictedSessionEnded, "restricted session ended")
	}()
	for deadline.Err() == nil {
		if err := ac.handleControl(deadline); err != nil {
			break
		}
	}
	closeWithCode(conn, protocol.CloseRestrictedSessionEnded, "restricted session ended")
}

// dispatchRestricted handles the three messages a restricted connection may send and drops the
// rest: registration offers a renewal, the request is answered, the install notice ends it.
func (a *AgentConn) dispatchRestricted(ctx context.Context, msg *protocol.ControlMessage) error {
	switch msg.Type {
	case protocol.MsgAgentRegister:
		a.setMeta(msg.OS, msg.Arch, msg.Version, msg.Capabilities)
		a.offerRenewal(ctx)
		return nil
	case protocol.MsgCertificateSigningRequest:
		return a.handleCertificateRequest(ctx, msg)
	case protocol.MsgCertificateInstalled:
		if err := a.handleCertificateInstalled(ctx); err != nil {
			return err
		}
		if a.quicConn != nil {
			closeWithCode(a.quicConn, protocol.CloseRestrictedSessionEnded, "renewed")
		}
		return nil
	default:
		return nil
	}
}
