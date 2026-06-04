// Package transport implements the Intel AMT Management Presence Server.
//
// MPS accepts CIRA (Client Initiated Remote Access) connections from Intel AMT
// devices over TLS. It speaks the APF (AMT Port Forwarding) protocol and
// manages per-device connections and TCP channel forwarding.
//
// This file holds the server type and connection lifecycle. The APF handshake
// lives in mps_handshake.go, post-handshake message dispatch in mps_handlers.go,
// and the Conn/Channel types in mps_conn.go. The APF wire codec is in apf*.go.
package transport

import (
	"context"
	"crypto/tls"
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

// AMTStateWriter is the narrow port mps uses to persist device online/offline
// state when CIRA connections come and go. The amt.Repository (ADR-021 #6)
// satisfies this interface; mps does NOT import amt to avoid a cycle —
// amt.Service holds a *mps.Server, so the dependency direction must stay
// amt → mps. The method names mirror amt.Repository so the interface is
// satisfied structurally.
type AMTStateWriter interface {
	Upsert(ctx context.Context, d *db.AMTDevice) error
	SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error
}

// Server is the Intel AMT Management Presence Server.
type Server struct {
	cert   *cert.Manager
	state  AMTStateWriter
	conns  sync.Map // map[uuid.UUID]*Conn
	count  atomic.Int64
	logger *slog.Logger
	addrCh chan string
	once   sync.Once
}

// NewServer creates a new MPS server.
func NewServer(cm *cert.Manager, state AMTStateWriter, logger *slog.Logger) *Server {
	return &Server{
		cert:   cm,
		state:  state,
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
		_ = ln.Close()
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

	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()

	s.registerConn(connCtx, mc, amtUUID)
	defer s.unregisterConn(mc, amtUUID)

	go s.startKeepalive(connCtx, mc)

	s.messageLoop(connCtx, mc)
}

// registerConn stores the connection and marks the device online.
func (s *Server) registerConn(ctx context.Context, mc *Conn, amtUUID uuid.UUID) {
	s.conns.Store(amtUUID, mc)
	s.count.Add(1)

	upsertCtx, upsertCancel := context.WithTimeout(ctx, 5*time.Second)
	defer upsertCancel()
	if err := s.state.Upsert(upsertCtx, &db.AMTDevice{
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
	if err := s.state.SetStatus(offCtx, amtUUID, db.StatusOffline); err != nil {
		mc.logger.Error("set AMT device offline", "error", err)
	}
	mc.logger.Info("AMT device disconnected")
}

// startKeepalive sends periodic keepalive requests to the AMT device.
func (s *Server) startKeepalive(ctx context.Context, mc *Conn) {
	// Negotiate keepalive parameters: 30s interval, 10s timeout.
	if err := WriteKeepaliveOptionsRequest(mc.netConn, 30, 10); err != nil {
		mc.logger.Error("write keepalive options", "error", err)
		return
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var cookie uint32
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cookie++
			if err := WriteKeepaliveRequest(mc.netConn, cookie); err != nil {
				return
			}
		}
	}
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
