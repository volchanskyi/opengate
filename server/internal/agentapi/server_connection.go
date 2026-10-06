package agentapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// accept handles a single QUIC connection.
func (s *AgentServer) accept(ctx context.Context, conn *quic.Conn) {
	logger := s.logger.With("remote_addr", conn.RemoteAddr())

	stream, err := s.acceptControlStream(ctx, conn, logger)
	if err != nil {
		return
	}

	result, err := s.performHandshake(ctx, conn, stream, logger)
	if err != nil {
		return
	}

	logger = logger.With("device_id", result.DeviceID)
	logger.Info("handshake complete", "fast_path", result.Skipped)

	if s.rejectIfTombstoned(stream, conn, result.DeviceID, logger) {
		return
	}

	ctx = s.scopeForDevice(ctx, result.DeviceID, logger)
	siteID, hostname := s.lookupDeviceMeta(ctx, result.DeviceID)

	deviceID := result.DeviceID
	ac := &AgentConn{
		DeviceID:      deviceID,
		TenantID:      agentTenantID(ctx),
		SiteID:        siteID,
		isTombstoned:  func() bool { _, ok := s.tombstones.Load(deviceID); return ok },
		stream:        stream,
		codec:         &protocol.Codec{},
		devices:       s.devices,
		hardware:      s.hardware,
		deviceUpdates: s.deviceUpdates,
		telemetry:     s.telemetry,
		processes:     s.processes,
		inventory:     s.inventory,
		scheduler:     s.scheduler,
		alertRules:    s.alertRules,
		alertStore:    s.alertStore,
		ruleCatalog:   s.ruleCatalog,
		coverage:      s.coverage,
		ruleCoverage:  s.ruleCoverage,
		settings:      s.settings,
		metrics:       s.metrics,
		logger:        logger,
	}

	s.registerConn(ctx, ac, hostname)
	defer s.unregisterConn(stream, conn, ac, hostname, logger)
	s.runControlLoop(ctx, ac, logger)
}

// registerConn stores the connection in the server map and emits an online event.
// The count follows the machine, so a replacement connection is not counted as a second arrival.
func (s *AgentServer) registerConn(ctx context.Context, ac *AgentConn, hostname string) {
	leave := s.statusGate.enter(ac.DeviceID)
	if _, replaced := s.conns.Swap(ac.DeviceID, ac); !replaced {
		s.count.Add(1)
	}
	leave()
	onlineEvt := notifications.Event{
		Type:           notifications.EventDeviceOnline,
		DeviceID:       ac.DeviceID,
		DeviceHostname: hostname,
		Timestamp:      time.Now(),
	}
	_ = s.notifier.Notify(ctx, onlineEvt)
}

// unregisterConn releases the device's status and closes the stream and
// connection.
func (s *AgentServer) unregisterConn(stream *quic.Stream, conn *quic.Conn, ac *AgentConn, hostname string, logger *slog.Logger) {
	// Runs first so handlers still holding this connection stop writing for the machine.
	ac.markReleased()
	s.scheduler.Release(ac.DeviceID)
	s.coverage.Forget(ac.DeviceID)
	s.releaseDeviceStatus(ac, hostname, logger)
	_ = stream.Close()
	_ = conn.CloseWithError(0, "bye")
	logger.Info("agent disconnected")
}

// releaseDeviceStatus marks the device offline when this connection is still the one held for it.
// The map check and the status write share the device's gate so a reconnect is never overwritten.
func (s *AgentServer) releaseDeviceStatus(ac *AgentConn, hostname string, logger *slog.Logger) {
	defer s.statusGate.enter(ac.DeviceID)()

	if !s.conns.CompareAndDelete(ac.DeviceID, ac) {
		logger.Info("skipping offline transition, newer connection exists")
		return
	}
	s.count.Add(-1)
	offlineCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if ac.TenantID != uuid.Nil {
		offlineCtx = dbtx.WithTenant(offlineCtx, ac.TenantID, false)
	} else {
		offlineCtx = dbtx.WithDefaultTenant(offlineCtx, false)
	}
	if err := s.devices.SetStatus(offlineCtx, ac.DeviceID, device.StatusOffline); err != nil {
		logger.Error("set device offline", "error", err)
	}
	offlineEvt := notifications.Event{
		Type:           notifications.EventDeviceOffline,
		DeviceID:       ac.DeviceID,
		DeviceHostname: hostname,
		Timestamp:      time.Now(),
	}
	_ = s.notifier.Notify(offlineCtx, offlineEvt)
}

// deviceStatusGate serializes one device's status transitions.
// A lock exists only while a transition is in flight, and the zero value is ready to use.
type deviceStatusGate struct {
	mu    sync.Mutex
	locks map[protocol.DeviceID]*deviceStatusLock
}

type deviceStatusLock struct {
	mu      sync.Mutex
	waiting int
}

// enter blocks until the device's gate is free and returns the function that leaves it.
func (g *deviceStatusGate) enter(id protocol.DeviceID) func() {
	g.mu.Lock()
	if g.locks == nil {
		g.locks = make(map[protocol.DeviceID]*deviceStatusLock)
	}
	lock, ok := g.locks[id]
	if !ok {
		lock = &deviceStatusLock{}
		g.locks[id] = lock
	}
	lock.waiting++
	g.mu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		g.mu.Lock()
		defer g.mu.Unlock()
		lock.waiting--
		if lock.waiting == 0 {
			delete(g.locks, id)
		}
	}
}

// inFlight names the devices currently holding a gate.
func (g *deviceStatusGate) inFlight() []protocol.DeviceID {
	g.mu.Lock()
	defer g.mu.Unlock()
	ids := make([]protocol.DeviceID, 0, len(g.locks))
	for id := range g.locks {
		ids = append(ids, id)
	}
	return ids
}

func (s *AgentServer) scopeForDevice(ctx context.Context, deviceID uuid.UUID, logger *slog.Logger) context.Context {
	resolveCtx := dbtx.WithDefaultTenant(ctx, true)
	tenantID, err := s.devices.TenantForDevice(resolveCtx, deviceID)
	if err != nil {
		if !errors.Is(err, device.ErrDeviceNotFound) {
			logger.Warn("resolve device tenant failed; falling back to default tenant", "error", err)
		}
		return dbtx.WithDefaultTenant(ctx, false)
	}
	return dbtx.WithTenant(ctx, tenantID, false)
}

func agentTenantID(ctx context.Context) uuid.UUID {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		return uuid.Nil
	}
	return tenant.TenantID
}

// runControlLoop processes control messages until the stream errors or the context is cancelled.
func (s *AgentServer) runControlLoop(ctx context.Context, ac *AgentConn, logger *slog.Logger) {
	// WithoutCancel keeps the tenant scope while the flush outlives the cancelled loop context.
	defer ac.flushTelemetry(context.WithoutCancel(ctx))
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

// acceptControlStream accepts the agent-opened control stream and closes the connection on error.
func (s *AgentServer) acceptControlStream(ctx context.Context, conn *quic.Conn, logger *slog.Logger) (*quic.Stream, error) {
	acceptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	stream, err := conn.AcceptStream(acceptCtx)
	if err != nil {
		logger.Error("accept control stream", "error", err)
		_ = conn.CloseWithError(1, "stream accept failed")
		return nil, err
	}
	return stream, nil
}

// performHandshake performs the agent handshake on the given stream. On failure
// it closes the connection.
func (s *AgentServer) performHandshake(ctx context.Context, conn *quic.Conn, stream *quic.Stream, logger *slog.Logger) (*HandshakeResult, error) {
	tlsState := conn.ConnectionState().TLS
	// Counted before the application handshake so the series covers every TLS handshake.
	if s.metrics != nil {
		s.metrics.ObserveAgentTLSHandshake(tlsState.DidResume)
	}
	peerCerts := make([][]byte, len(tlsState.PeerCertificates))
	for i, c := range tlsState.PeerCertificates {
		peerCerts[i] = c.Raw
	}

	handshaker := NewHandshaker(s.cert)
	hsCtx, hsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer hsCancel()
	result, err := handshaker.PerformHandshake(hsCtx, stream, peerCerts)
	if err != nil {
		logger.Error("handshake failed", "error", err)
		_ = conn.CloseWithError(2, "handshake failed")
		return nil, err
	}
	return result, nil
}

// rejectIfTombstoned closes the connection with a deregister message if the
// device has been tombstoned. Returns true if the device was rejected.
func (s *AgentServer) rejectIfTombstoned(stream *quic.Stream, conn *quic.Conn, deviceID uuid.UUID, logger *slog.Logger) bool {
	if _, tombstoned := s.tombstones.Load(deviceID); !tombstoned {
		return false
	}
	logger.Info("rejecting tombstoned device")
	codec := &protocol.Codec{}
	msg := &protocol.ControlMessage{
		Type:   protocol.MsgAgentDeregistered,
		Reason: "device deleted by administrator",
	}
	if payload, err := codec.EncodeControl(msg); err != nil {
		logger.Warn("encode tombstone deregister", "error", err)
	} else if err := codec.WriteFrame(stream, protocol.FrameControl, payload); err != nil {
		logger.Warn("write tombstone deregister frame", "error", err)
	}
	_ = stream.Close()
	_ = conn.CloseWithError(3, "device deregistered")
	return true
}

// lookupDeviceMeta resolves the site and hostname for a device, falling back
// to defaults if the device is not yet persisted.
func (s *AgentServer) lookupDeviceMeta(ctx context.Context, deviceID uuid.UUID) (uuid.UUID, string) {
	siteID := uuid.Nil
	hostname := deviceID.String()[:8]
	if existing, err := s.devices.Get(ctx, deviceID); err == nil {
		siteID = existing.SiteID
		if existing.Hostname != "" {
			hostname = existing.Hostname
		}
	}
	return siteID, hostname
}
