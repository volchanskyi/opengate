// Package mps implements the Intel AMT Management Presence Server.
//
// MPS accepts CIRA (Client Initiated Remote Access) connections from Intel AMT
// devices over TLS. It speaks the APF (AMT Port Forwarding) protocol and
// manages per-device connections and TCP channel forwarding.
package mps

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
)

// Server is the Intel AMT Management Presence Server.
type Server struct {
	cert   *cert.Manager
	store  db.Store
	conns  sync.Map // map[uuid.UUID]*Conn
	count  atomic.Int64
	logger *slog.Logger
	addrCh chan string
	once   sync.Once
}

// NewServer creates a new MPS server.
func NewServer(cm *cert.Manager, store db.Store, logger *slog.Logger) *Server {
	return &Server{
		cert:   cm,
		store:  store,
		logger: logger,
		addrCh: make(chan string, 1),
	}
}

// ConnectedDeviceCount returns the number of active AMT connections.
func (s *Server) ConnectedDeviceCount() int {
	return int(s.count.Load())
}

// GetConn returns the CIRA connection for the given AMT device UUID.
func (s *Server) GetConn(amtUUID uuid.UUID) *Conn {
	val, ok := s.conns.Load(amtUUID)
	if !ok {
		return nil
	}
	return val.(*Conn)
}

// Addr blocks until the server is listening and returns the actual address.
func (s *Server) Addr() string {
	return <-s.addrCh
}

// ListenAndServe starts the TLS listener and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	tlsCfg, err := s.cert.MPSTLSConfig()
	if err != nil {
		return fmt.Errorf("MPS TLS config: %w", err)
	}

	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("MPS listen: %w", err)
	}
	defer ln.Close()

	actualAddr := ln.Addr().String()
	s.once.Do(func() {
		s.addrCh <- actualAddr
		close(s.addrCh)
	})

	s.logger.Info("MPS server listening", "addr", actualAddr)

	// Close listener when context is done.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			s.logger.Error("MPS accept error", "error", err)
			continue
		}

		go s.handleConn(ctx, conn)
	}
}

// handleConn processes one CIRA connection through the APF handshake and
// enters the message loop.
func (s *Server) handleConn(ctx context.Context, netConn net.Conn) {
	logger := s.logger.With("remote_addr", netConn.RemoteAddr())
	logger.Info("AMT device connected")

	mc := &Conn{
		netConn:  netConn,
		channels: make(map[uint32]*Channel),
		logger:   logger,
	}
	defer mc.Close()

	amtUUID, err := s.handshake(mc)
	if err != nil {
		logger.Error("CIRA handshake failed", "error", err)
		return
	}

	mc.AMTUUID = amtUUID
	mc.logger = logger.With("amt_uuid", amtUUID)
	mc.logger.Info("CIRA handshake complete")

	s.registerConn(ctx, mc, amtUUID)
	defer s.unregisterConn(mc, amtUUID)

	s.messageLoop(ctx, mc)
}

// registerConn stores the connection and marks the device online.
func (s *Server) registerConn(ctx context.Context, mc *Conn, amtUUID uuid.UUID) {
	s.conns.Store(amtUUID, mc)
	s.count.Add(1)

	upsertCtx, upsertCancel := context.WithTimeout(ctx, 5*time.Second)
	defer upsertCancel()
	if err := s.store.UpsertAMTDevice(upsertCtx, &db.AMTDevice{
		UUID:     amtUUID,
		Status:   db.StatusOnline,
		LastSeen: time.Now(),
	}); err != nil {
		mc.logger.Error("upsert AMT device", "error", err)
	}
}

// unregisterConn removes the connection and marks the device offline.
func (s *Server) unregisterConn(mc *Conn, amtUUID uuid.UUID) {
	s.conns.Delete(amtUUID)
	s.count.Add(-1)

	offCtx, offCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer offCancel()
	if err := s.store.SetAMTDeviceStatus(offCtx, amtUUID, db.StatusOffline); err != nil {
		mc.logger.Error("set AMT device offline", "error", err)
	}
	mc.logger.Info("AMT device disconnected")
}

// messageLoop reads and dispatches APF messages until error or context cancel.
func (s *Server) messageLoop(ctx context.Context, mc *Conn) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := mc.netConn.SetReadDeadline(time.Now().Add(90 * time.Second)); err != nil {
			return
		}

		msgType, payload, err := ReadMessage(mc.netConn)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			mc.logger.Error("read APF message", "error", err)
			return
		}

		if err := s.handleMessage(mc, msgType, payload); err != nil {
			mc.logger.Error("handle APF message", "type", msgType, "error", err)
			return
		}
	}
}

// handshake performs the CIRA APF handshake sequence:
// 1. ProtocolVersion exchange → 2. Auth service → 3. UserAuth →
// 4. PFwd service → 5. GlobalRequest (tcpip-forward)
func (s *Server) handshake(mc *Conn) (uuid.UUID, error) {
	if err := mc.netConn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return uuid.Nil, err
	}
	defer mc.netConn.SetDeadline(time.Time{}) //nolint:errcheck

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
	return pv.UUID, nil
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
	mc.logger.Info("AMT forward request", "request", gr.RequestName)
	if gr.WantReply {
		return WriteRequestSuccess(mc.netConn)
	}
	return nil
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

// handleMessage dispatches a post-handshake APF message.
func (s *Server) handleMessage(mc *Conn, msgType uint8, payload []byte) error {
	switch msgType {
	case APFGlobalRequest:
		gr, err := ParseGlobalRequest(payload)
		if err != nil {
			return err
		}
		mc.logger.Debug("global request", "request", gr.RequestName)
		if gr.WantReply {
			return WriteRequestSuccess(mc.netConn)
		}
		return nil

	case APFChannelOpen:
		return s.handleChannelOpen(mc, payload)

	case APFChannelData:
		return s.handleChannelData(mc, payload)

	case APFChannelClose:
		if len(payload) < 4 {
			return ErrMessageTooShort
		}
		ch := binary.BigEndian.Uint32(payload)
		return s.handleChannelClose(mc, ch)

	case APFChannelWindowAdj:
		// Acknowledge but don't enforce flow control for now.
		return nil

	case APFDisconnect:
		return io.EOF // Clean disconnect.

	default:
		mc.logger.Warn("unhandled APF message", "type", msgType)
		return nil
	}
}

// handleChannelOpen processes an APF channel open request from the AMT device.
func (s *Server) handleChannelOpen(mc *Conn, payload []byte) error {
	co, err := ParseChannelOpen(payload)
	if err != nil {
		return err
	}

	mc.mu.Lock()
	localCh := mc.nextChanID
	mc.nextChanID++
	ch := &Channel{
		LocalID:  localCh,
		RemoteID: co.SenderChannel,
		Type:     co.ChannelType,
	}
	mc.channels[localCh] = ch
	mc.mu.Unlock()

	mc.logger.Info("channel opened",
		"type", co.ChannelType,
		"local_ch", localCh,
		"remote_ch", co.SenderChannel)

	return WriteChannelOpenConfirm(mc.netConn,
		co.SenderChannel, localCh,
		DefaultWindowSize, DefaultMaxPacketSize)
}

// handleChannelData processes data received on an APF channel.
func (s *Server) handleChannelData(mc *Conn, payload []byte) error {
	cd, err := ParseChannelData(payload)
	if err != nil {
		return err
	}

	mc.mu.RLock()
	ch, ok := mc.channels[cd.RecipientChannel]
	mc.mu.RUnlock()

	if !ok {
		mc.logger.Warn("data for unknown channel", "channel", cd.RecipientChannel)
		return nil
	}

	// Forward data to the channel's TCP connection if one exists.
	ch.mu.Lock()
	fwd := ch.fwd
	ch.mu.Unlock()

	if fwd != nil {
		if _, err := fwd.Write(cd.Data); err != nil {
			mc.logger.Error("forward write error", "channel", cd.RecipientChannel, "error", err)
			return s.handleChannelClose(mc, cd.RecipientChannel)
		}
	}

	return nil
}

// handleChannelClose closes a channel and its forwarding connection.
func (s *Server) handleChannelClose(mc *Conn, localCh uint32) error {
	mc.mu.Lock()
	ch, ok := mc.channels[localCh]
	if ok {
		delete(mc.channels, localCh)
	}
	mc.mu.Unlock()

	if !ok {
		return nil
	}

	ch.mu.Lock()
	if ch.fwd != nil {
		ch.fwd.Close()
	}
	ch.mu.Unlock()

	mc.logger.Info("channel closed", "local_ch", localCh)
	return WriteChannelClose(mc.netConn, ch.RemoteID)
}

// Conn represents a CIRA connection from an Intel AMT device.
type Conn struct {
	AMTUUID    uuid.UUID
	netConn    net.Conn
	channels   map[uint32]*Channel
	nextChanID uint32
	mu         sync.RWMutex
	logger     *slog.Logger
}

// Close terminates the CIRA connection and all open channels.
func (c *Conn) Close() error {
	c.mu.Lock()
	for _, ch := range c.channels {
		ch.mu.Lock()
		if ch.fwd != nil {
			ch.fwd.Close()
		}
		ch.mu.Unlock()
	}
	c.channels = nil
	c.mu.Unlock()
	return c.netConn.Close()
}

// OpenChannel opens a new channel to the AMT device for port forwarding.
// It sends a channel open request and waits for confirmation.
func (c *Conn) OpenChannel(targetAddr string, targetPort uint16) (*Channel, error) {
	c.mu.Lock()
	localCh := c.nextChanID
	c.nextChanID++
	c.mu.Unlock()

	// Build channel open message for "direct-tcpip".
	if err := writeChannelOpenDirect(c.netConn, localCh, targetAddr, targetPort); err != nil {
		return nil, fmt.Errorf("write channel open: %w", err)
	}

	// Read the response.
	if err := c.netConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	defer c.netConn.SetReadDeadline(time.Time{}) //nolint:errcheck

	msgType, payload, err := ReadMessage(c.netConn)
	if err != nil {
		return nil, fmt.Errorf("read channel open response: %w", err)
	}

	if msgType == APFChannelOpenFailure {
		reason := uint32(0)
		if len(payload) >= 8 {
			reason = binary.BigEndian.Uint32(payload[4:8])
		}
		return nil, fmt.Errorf("channel open rejected: reason %d", reason)
	}

	if msgType != APFChannelOpenConfirm {
		return nil, fmt.Errorf("unexpected response type %d", msgType)
	}

	if len(payload) < 8 {
		return nil, ErrMessageTooShort
	}
	remoteCh := binary.BigEndian.Uint32(payload[4:8])

	ch := &Channel{
		LocalID:  localCh,
		RemoteID: remoteCh,
		Type:     "direct-tcpip",
	}

	c.mu.Lock()
	c.channels[localCh] = ch
	c.mu.Unlock()

	return ch, nil
}

// Channel represents an APF channel within a CIRA connection.
type Channel struct {
	LocalID  uint32
	RemoteID uint32
	Type     string
	fwd      net.Conn // optional TCP forwarding connection
	mu       sync.Mutex
}

// writeChannelOpenDirect writes an APF channel open for "direct-tcpip".
func writeChannelOpenDirect(w io.Writer, senderCh uint32, addr string, port uint16) error {
	chType := "direct-tcpip"
	// Build: type_str + sender_ch + window + max_pkt + connected_addr + connected_port + origin_addr + origin_port
	addrBytes := encodeAPFString(addr)
	originBytes := encodeAPFString("0.0.0.0")

	totalLen := 1 + 4 + len(chType) + 12 + len(addrBytes) + 4 + len(originBytes) + 4
	buf := make([]byte, totalLen)
	off := 0
	buf[off] = APFChannelOpen
	off++
	binary.BigEndian.PutUint32(buf[off:], uint32(len(chType)))
	off += 4
	copy(buf[off:], chType)
	off += len(chType)
	binary.BigEndian.PutUint32(buf[off:], senderCh)
	off += 4
	binary.BigEndian.PutUint32(buf[off:], DefaultWindowSize)
	off += 4
	binary.BigEndian.PutUint32(buf[off:], DefaultMaxPacketSize)
	off += 4
	copy(buf[off:], addrBytes)
	off += len(addrBytes)
	binary.BigEndian.PutUint32(buf[off:], uint32(port))
	off += 4
	copy(buf[off:], originBytes)
	off += len(originBytes)
	binary.BigEndian.PutUint32(buf[off:], 0) // origin port

	_, err := w.Write(buf)
	return err
}

func encodeAPFString(s string) []byte {
	buf := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(buf, uint32(len(s)))
	copy(buf[4:], s)
	return buf
}
