package agentapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"net"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
)

// AgentServer accepts QUIC connections from agents and manages their lifecycle.
type AgentServer struct {
	cert     *cert.Manager
	store    db.Store
	relay    *relay.Relay
	notifier notifications.Notifier
	conns    sync.Map // map[protocol.DeviceID]*AgentConn
	count    atomic.Int64
	logger   *slog.Logger
	addrCh   chan string // signals the actual listen address
	addrOnce sync.Once
}

// NewAgentServer creates a new AgentServer.
func NewAgentServer(cm *cert.Manager, store db.Store, r *relay.Relay, notifier notifications.Notifier, logger *slog.Logger) *AgentServer {
	return &AgentServer{
		cert:     cm,
		store:    store,
		relay:    r,
		notifier: notifier,
		logger:   logger,
		addrCh:   make(chan string, 1),
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

// Addr blocks until the server is listening and returns the actual address.
func (s *AgentServer) Addr() string {
	return <-s.addrCh
}

// ListenAndServe starts the QUIC listener and blocks until ctx is cancelled.
func (s *AgentServer) ListenAndServe(ctx context.Context, addr string) error {
	tlsCfg, err := s.cert.ServerTLSConfig()
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

	// Accept connections until context is cancelled
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

// accept handles a single QUIC connection.
func (s *AgentServer) accept(ctx context.Context, conn quic.Connection) {
	logger := s.logger.With("remote_addr", conn.RemoteAddr())

	// Extract peer certificates from TLS state.
	tlsState := conn.ConnectionState().TLS
	peerCerts := make([][]byte, len(tlsState.PeerCertificates))
	for i, c := range tlsState.PeerCertificates {
		peerCerts[i] = c.Raw
	}

	// Open the control stream (server-initiated).
	// With mTLS, AcceptStream blocks until the client writes data,
	// but the protocol requires the server to send ServerHello first.
	// So the server opens the stream and the client accepts it.
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		logger.Error("open control stream", "error", err)
		conn.CloseWithError(1, "stream open failed")
		return
	}

	// Perform handshake.
	handshaker := NewHandshaker(s.cert)
	hsCtx, hsCancel := context.WithTimeout(ctx, 10*time.Second)
	result, err := handshaker.PerformHandshake(hsCtx, stream, peerCerts)
	hsCancel()

	if err != nil {
		logger.Error("handshake failed", "error", err)
		conn.CloseWithError(2, "handshake failed")
		return
	}

	logger = logger.With("device_id", result.DeviceID)
	logger.Info("handshake complete")

	// Determine the group and hostname for this device.
	groupID := uuid.Nil
	hostname := result.DeviceID.String()[:8]
	if existing, err := s.store.GetDevice(ctx, result.DeviceID); err == nil {
		groupID = existing.GroupID
		if existing.Hostname != "" {
			hostname = existing.Hostname
		}
	}

	// Create AgentConn.
	ac := &AgentConn{
		DeviceID: result.DeviceID,
		GroupID:  groupID,
		stream:   stream,
		codec:    &protocol.Codec{},
		store:    s.store,
		logger:   logger,
	}

	// Register and start the control loop.
	s.conns.Store(result.DeviceID, ac)
	s.count.Add(1)

	_ = s.notifier.Notify(ctx, notifications.Event{
		Type:           notifications.EventDeviceOnline,
		DeviceID:       result.DeviceID,
		DeviceHostname: hostname,
		Timestamp:      time.Now(),
	})

	defer func() {
		s.conns.Delete(result.DeviceID)
		s.count.Add(-1)

		// Set device offline.
		offlineCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.store.SetDeviceStatus(offlineCtx, result.DeviceID, db.StatusOffline); err != nil {
			logger.Error("set device offline", "error", err)
		}

		_ = s.notifier.Notify(offlineCtx, notifications.Event{
			Type:           notifications.EventDeviceOffline,
			DeviceID:       result.DeviceID,
			DeviceHostname: hostname,
			Timestamp:      time.Now(),
		})

		stream.Close()
		conn.CloseWithError(0, "bye")
		logger.Info("agent disconnected")
	}()

	// Run the control loop.
	for {
		if err := ac.handleControl(ctx); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || ctx.Err() != nil {
				return
			}
			logger.Error("control loop error", "error", err)
			return
		}
	}
}

