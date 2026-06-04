package transport

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// This file holds the CIRA/APF handshake sequence. The server core and
// connection lifecycle live in mps.go; post-handshake message dispatch in
// mps_handlers.go; the Conn/Channel types in mps_conn.go.

// handshake performs the CIRA APF handshake sequence:
// 1. ProtocolVersion exchange → 2. Auth service → 3. UserAuth →
// 4. PFwd service → 5. GlobalRequest (tcpip-forward)
func (s *Server) handshake(mc *Conn) (uuid.UUID, error) {
	if err := mc.netConn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return uuid.Nil, err
	}
	// Reset deadline on exit. SetDeadline only fails on a closed conn, in which
	// case the handshake error path has already taken over and the result here
	// is irrelevant — safe to ignore.
	defer func() { _ = mc.netConn.SetDeadline(time.Time{}) }()

	amtUUID, err := s.hsExchangeVersion(mc)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.hsAuthService(mc); err != nil {
		return uuid.Nil, err
	}
	if err := s.hsPfwdService(mc); err != nil {
		return uuid.Nil, err
	}
	return amtUUID, nil
}

// hsExchangeVersion handles protocol version exchange and user auth.
func (s *Server) hsExchangeVersion(mc *Conn) (uuid.UUID, error) {
	msgType, payload, err := ReadMessage(mc.netConn)
	if err != nil {
		return uuid.Nil, fmt.Errorf("read protocol version: %w", err)
	}
	if msgType != APFProtocolVersion {
		return uuid.Nil, fmt.Errorf("expected protocol version (192), got %d", msgType)
	}
	pv, err := ParseProtocolVersion(payload)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse protocol version: %w", err)
	}
	mc.logger.Info("AMT protocol version", "major", pv.MajorVersion, "minor", pv.MinorVersion)

	if err := WriteProtocolVersion(mc.netConn, 1, 0, 0); err != nil {
		return uuid.Nil, fmt.Errorf("write protocol version: %w", err)
	}
	return ReorderIntelGUID(pv.UUID), nil
}

// hsAuthService handles the auth service request and user authentication.
func (s *Server) hsAuthService(mc *Conn) error {
	if err := expectServiceRequest(mc, ServiceAuth); err != nil {
		return err
	}
	// User auth.
	msgType, payload, err := ReadMessage(mc.netConn)
	if err != nil {
		return fmt.Errorf("read user auth: %w", err)
	}
	if msgType != APFUserAuthRequest {
		return fmt.Errorf("expected user auth request (50), got %d", msgType)
	}
	ua, err := ParseUserAuthRequest(payload)
	if err != nil {
		return err
	}
	mc.logger.Info("AMT auth", "username", ua.Username, "method", ua.MethodName)
	return WriteUserAuthSuccess(mc.netConn)
}

// hsPfwdService handles the port-forwarding service request and initial global request.
func (s *Server) hsPfwdService(mc *Conn) error {
	if err := expectServiceRequest(mc, ServicePFwd); err != nil {
		return err
	}
	// Global request (tcpip-forward).
	msgType, payload, err := ReadMessage(mc.netConn)
	if err != nil {
		return fmt.Errorf("read global request: %w", err)
	}
	if msgType != APFGlobalRequest {
		return fmt.Errorf("expected global request (80), got %d", msgType)
	}
	gr, err := ParseGlobalRequest(payload)
	if err != nil {
		return err
	}
	mc.logger.Debug("handshake forward request", "request", gr.RequestName)
	recordBoundPort(mc, &gr)
	if gr.WantReply {
		return WriteRequestSuccess(mc.netConn)
	}
	return nil
}

// recordBoundPort parses a tcpip-forward global request and records the bound port.
func recordBoundPort(mc *Conn, gr *GlobalRequest) {
	if gr.RequestName != "tcpip-forward" || len(gr.Data) == 0 {
		return
	}
	addr, port, err := ParseForwardData(gr.Data)
	if err != nil {
		return
	}
	mc.mu.Lock()
	mc.BoundPorts = append(mc.BoundPorts, BoundPort{Address: addr, Port: port})
	mc.mu.Unlock()
}

// expectServiceRequest reads and validates an APF service request, then accepts it.
func expectServiceRequest(mc *Conn, expected string) error {
	msgType, payload, err := ReadMessage(mc.netConn)
	if err != nil {
		return fmt.Errorf("read %s service request: %w", expected, err)
	}
	if msgType != APFServiceRequest {
		return fmt.Errorf("expected service request (5), got %d", msgType)
	}
	sr, err := ParseServiceRequest(payload)
	if err != nil {
		return err
	}
	if sr.ServiceName != expected {
		return fmt.Errorf("expected %s service, got %q", expected, sr.ServiceName)
	}
	return WriteServiceAccept(mc.netConn, expected)
}
