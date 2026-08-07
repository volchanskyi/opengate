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
	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// amtLinkTimeout bounds the device lookup and the connection-state writes that
// follow it. amtProbeTimeout bounds the WSMAN detail read, which crosses the
// CIRA tunnel to the device itself and so deserves more room.
const (
	amtLinkTimeout  = 5 * time.Second
	amtProbeTimeout = 30 * time.Second
)

// keepaliveInterval is the APF keepalive cadence negotiated with the device.
// defaultRelinkInterval is how often an unlinked connection retries its device
// lookup, so a machine whose agent registers after its AMT firmware dialled in
// is adopted without reconnecting.
const (
	keepaliveInterval     = 30 * time.Second
	defaultRelinkInterval = 30 * time.Second
)

// AMTStateWriter is the narrow port mps uses to persist device online/offline
// state when CIRA connections come and go. The amt.Repository
// satisfies this interface; mps does NOT import amt to avoid a cycle —
// amt.Service holds a *mps.Server, so the dependency direction must stay
// amt → mps. The method names mirror amt.Repository so the interface is
// satisfied structurally.
type AMTStateWriter interface {
	Upsert(ctx context.Context, d *db.AMTDevice) error
	SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error
}

// AMTDeviceLinker resolves which managed device a CIRA connection belongs to and
// files the detail read back over it. A CIRA connection carries no request
// tenant, so this lookup is what supplies one: it maps the AMT firmware's UUID —
// the host's SMBIOS system UUID on vPro hardware — to a device and its
// tenant. device.HardwareRepository satisfies it structurally, by the same
// no-import rule as AMTStateWriter.
type AMTDeviceLinker interface {
	ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (uuid.UUID, uuid.UUID, error)
	SetAMTDetail(ctx context.Context, deviceID uuid.UUID, model, firmware string) error
}

// AMTDetailProber reads a connected device's machine model and AMT firmware
// version over WSMAN. amt.Service implements it and is wired in after
// construction, because amt.Service itself holds the MPS server.
type AMTDetailProber interface {
	ProbeDetail(ctx context.Context, mc *Conn) (string, string, error)
}

// Server is the Intel AMT Management Presence Server.
type Server struct {
	cert     *cert.Manager
	state    AMTStateWriter
	linker   AMTDeviceLinker
	proberMu sync.RWMutex
	prober   AMTDetailProber
	conns    sync.Map // map[uuid.UUID]*Conn
	count    atomic.Int64
	logger   *slog.Logger
	addrCh   chan string
	once     sync.Once

	// relinkInterval paces the retry for unlinked connections.
	relinkInterval time.Duration
}

// NewServer creates a new MPS server.
func NewServer(cm *cert.Manager, state AMTStateWriter, linker AMTDeviceLinker, logger *slog.Logger) *Server {
	return &Server{
		cert:           cm,
		state:          state,
		linker:         linker,
		logger:         logger,
		addrCh:         make(chan string, 1),
		relinkInterval: defaultRelinkInterval,
	}
}

// SetDetailProber supplies the WSMAN reader used to fill in a linked device's
// machine model and AMT firmware. Without one the connection still links and
// still accepts power commands; only the two hardware attributes stay blank.
func (s *Server) SetDetailProber(p AMTDetailProber) {
	s.proberMu.Lock()
	defer s.proberMu.Unlock()
	s.prober = p
}

func (s *Server) detailProber() AMTDetailProber {
	s.proberMu.RLock()
	defer s.proberMu.RUnlock()
	return s.prober
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

// registerConn stores the connection and links it to the device that owns it.
func (s *Server) registerConn(ctx context.Context, mc *Conn, amtUUID uuid.UUID) {
	s.conns.Store(amtUUID, mc)
	s.count.Add(1)

	if !s.linkConn(ctx, mc, amtUUID) {
		mc.logger.Info("AMT connection held unlinked: no managed device reports this system UUID")
	}
}

// linkConn resolves the managed device that owns this AMT connection and records
// the connection online under that device's tenant. It reports whether the
// connection is linked.
//
// A connection that resolves to nothing persists nothing: Intel AMT is a
// property of a managed device, so an AMT box with no agent has no tenant
// to store state in. The connection stays live in memory and the keepalive
// retries this lookup, so the machine is adopted the moment its agent registers.
func (s *Server) linkConn(ctx context.Context, mc *Conn, amtUUID uuid.UUID) bool {
	if _, _, ok := mc.linked(); ok {
		return true
	}

	linkCtx, cancel := context.WithTimeout(ctx, amtLinkTimeout)
	defer cancel()

	deviceID, tenantID, err := s.linker.ResolveBySystemUUID(linkCtx, amtUUID)
	if err != nil {
		return false
	}

	if err := s.state.Upsert(dbtx.WithTenant(linkCtx, tenantID, false), &db.AMTDevice{
		UUID:     amtUUID,
		DeviceID: deviceID,
		Status:   db.StatusOnline,
		LastSeen: time.Now(),
	}); err != nil {
		mc.logger.Error("upsert AMT device", "error", err)
		return false
	}

	mc.link(deviceID, tenantID)
	mc.logger.Info("AMT connection linked to device", "device_id", deviceID)
	go s.storeDetail(ctx, mc, deviceID, tenantID)
	return true
}

// storeDetail reads the machine model and AMT firmware version back over the
// CIRA connection and files them on the device's hardware row. It runs on its
// own goroutine because the WSMAN reply arrives through the message loop the
// caller is about to enter.
func (s *Server) storeDetail(ctx context.Context, mc *Conn, deviceID, tenantID uuid.UUID) {
	prober := s.detailProber()
	if prober == nil {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, amtProbeTimeout)
	defer cancel()

	model, firmware, err := prober.ProbeDetail(probeCtx, mc)
	if err != nil {
		mc.logger.Warn("probe AMT device detail", "error", err)
		return
	}
	if model == "" && firmware == "" {
		return
	}
	if err := s.linker.SetAMTDetail(dbtx.WithTenant(probeCtx, tenantID, false), deviceID, model, firmware); err != nil {
		mc.logger.Error("store AMT device detail", "error", err)
	}
}

// unregisterConn removes the connection and marks the device offline. An
// unlinked connection wrote no row, so there is nothing to mark.
func (s *Server) unregisterConn(mc *Conn, amtUUID uuid.UUID) {
	s.conns.Delete(amtUUID)
	s.count.Add(-1)

	if _, tenantID, ok := mc.linked(); ok {
		offCtx, offCancel := context.WithTimeout(context.Background(), amtLinkTimeout)
		defer offCancel()
		if err := s.state.SetStatus(dbtx.WithTenant(offCtx, tenantID, false), amtUUID, db.StatusOffline); err != nil {
			mc.logger.Error("set AMT device offline", "error", err)
		}
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

	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	// Retry the device lookup while the connection is unlinked, so an AMT box
	// that dialled in before its agent registered is adopted on the next tick
	// rather than on the next reconnect. linkConn returns immediately once the
	// connection is linked, so this costs nothing in the steady state.
	relink := time.NewTicker(s.relinkInterval)
	defer relink.Stop()

	var cookie uint32
	for {
		select {
		case <-ctx.Done():
			return
		case <-relink.C:
			s.linkConn(ctx, mc, mc.AMTUUID)
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
