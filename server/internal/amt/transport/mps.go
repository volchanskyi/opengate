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

// amtLinkTimeout bounds the device lookup and state writes; amtProbeTimeout bounds the
// WSMAN detail read, which crosses the CIRA tunnel to the device.
const (
	amtLinkTimeout  = 5 * time.Second
	amtProbeTimeout = 30 * time.Second
)

// keepaliveInterval is the APF keepalive cadence negotiated with the device;
// defaultRelinkInterval is how often an unlinked connection retries its device lookup.
const (
	keepaliveInterval     = 30 * time.Second
	defaultRelinkInterval = 30 * time.Second
)

// AMTStateWriter persists device online/offline state as CIRA connections come and go.
// It mirrors amt.Repository, which satisfies it structurally, because amt imports this package.
type AMTStateWriter interface {
	Upsert(ctx context.Context, d *db.AMTDevice) error
	SetStatus(ctx context.Context, id uuid.UUID, status db.DeviceStatus) error
}

// AMTDeviceLinker maps a CIRA connection's SMBIOS system UUID to its device and tenant,
// the only source of a tenant for a connection, and files the detail read on that device.
type AMTDeviceLinker interface {
	ResolveBySystemUUID(ctx context.Context, systemUUID uuid.UUID) (uuid.UUID, uuid.UUID, error)
	SetAMTDetail(ctx context.Context, deviceID uuid.UUID, model, firmware string) error
}

// AMTDetailProber reads a connected device's machine model and AMT firmware version over
// WSMAN; amt.Service implements it and is wired in after the server exists.
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

// SetDetailProber supplies the WSMAN reader that fills in a linked device's model and firmware.
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

// addrWait bounds how long Addr waits for the listener to bind.
const addrWait = 30 * time.Second

// Addr waits for the server to start listening and returns the bound address,
// or the empty string when the listener never bound within addrWait.
func (s *Server) Addr() string {
	addr, listening := waitForAddr(s.addrCh, addrWait)
	if !listening {
		s.logger.Error("the MPS listener did not bind", "waited", addrWait)
	}
	return addr
}

// waitForAddr reads the address the listener published, and reports whether one
// arrived before the wait ran out.
func waitForAddr(ch <-chan string, wait time.Duration) (string, bool) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case addr := <-ch:
		return addr, true
	case <-timer.C:
		return "", false
	}
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

// linkConn records the connection online under its device's tenant and reports whether it is
// linked. A connection with no matching device persists nothing and retries on the keepalive.
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

// storeDetail files the model and firmware read over the CIRA connection on the device's
// hardware row; it runs on its own goroutine because the reply arrives through the message loop.
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

	// An unlinked connection retries its device lookup on this tick; a linked one returns at once.
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
