package agentapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// One agent's connection, from the QUIC stream it opens to the moment the
// machine is marked offline.
//
// Every step here is a door the endpoint knocks on, so each is bounded and each
// failure closes the connection with a code rather than leaving it half-open:
// the stream is accepted under a timeout, the handshake runs under its own, a
// device the administrator deleted is turned away before it can register, and a
// teardown that finds a newer connection for the same machine leaves that one
// alone rather than marking a live machine offline.

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
//
// The count follows the machine rather than the connection. A machine that
// drops and dials straight back is registered while its previous connection is
// still tearing down, and that teardown will decline to decrement — it finds a
// newer connection in the map and leaves it alone. Counting the replacement as
// an arrival too would add one the process can never take back, and the
// connected-agents gauge would climb by one on every reconnect race for as long
// as the server runs.
func (s *AgentServer) registerConn(ctx context.Context, ac *AgentConn, hostname string) {
	if _, replaced := s.conns.Swap(ac.DeviceID, ac); !replaced {
		s.count.Add(1)
	}
	onlineEvt := notifications.Event{
		Type:           notifications.EventDeviceOnline,
		DeviceID:       ac.DeviceID,
		DeviceHostname: hostname,
		Timestamp:      time.Now(),
	}
	_ = s.notifier.Notify(ctx, onlineEvt) // fire-and-forget
}

// unregisterConn marks the device offline (if still owned by this connection)
// and closes the stream and connection.
func (s *AgentServer) unregisterConn(stream *quic.Stream, conn *quic.Conn, ac *AgentConn, hostname string, logger *slog.Logger) {
	// Free any backfill admission slot this connection held so a reconnect (or
	// another agent) can drain. Idempotent for agents that never backfilled, and
	// a no-op when the server carries no scheduler.
	s.scheduler.Release(ac.DeviceID)
	// A machine that drops off becomes unknown for every rule rather than
	// staying counted as one that is still being watched.
	s.coverage.Forget(ac.DeviceID)
	if s.conns.CompareAndDelete(ac.DeviceID, ac) {
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
		_ = s.notifier.Notify(offlineCtx, offlineEvt) // fire-and-forget
	} else {
		logger.Info("skipping offline transition, newer connection exists")
	}
	_ = stream.Close()
	_ = conn.CloseWithError(0, "bye")
	logger.Info("agent disconnected")
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
	// Flush any telemetry buffered since the last heartbeat on teardown so a
	// disconnect never silently drops the in-flight burst. WithoutCancel keeps
	// the connection's tenant scope while surviving the cancelled loop context.
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

// acceptControlStream accepts the agent-initiated control stream on the QUIC
// connection. The agent opens the stream and writes first; the server accepts
// it and replies during the handshake. On error, it closes the connection and
// returns the error.
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
	// Counted here, before the application handshake can fail, so the series is
	// TLS handshakes rather than successful registrations. Only this side knows
	// the answer: the machine cannot report whether its session resumed.
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
