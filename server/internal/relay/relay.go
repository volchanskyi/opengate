// Package relay implements the WebSocket relay that pipes browser and agent
// connections together.
package relay

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// defaultServerID is the serverID used when NewRelay is called without
// WithRegistry. Any non-empty stable value satisfies the in-process adapter.
const defaultServerID = "local"

var (
	// ErrDuplicateSide is returned when the same side of a session is registered twice.
	ErrDuplicateSide = errors.New("session side already registered")
	// ErrSessionNotFound is returned when a session token is not found.
	ErrSessionNotFound = errors.New("session not found")
)

// Side identifies which end of a relay session is connecting.
type Side int

const (
	// SideAgent is the agent end of a relay session.
	SideAgent Side = iota
	// SideBrowser is the browser end of a relay session.
	SideBrowser
)

// Conn is the interface used by the relay. Implementations must preserve
// message boundaries: each ReadMessage returns exactly one complete message,
// and each WriteMessage sends exactly one complete message.
type Conn interface {
	// ReadMessage reads one complete message. It blocks until a message is
	// available or an error occurs.
	ReadMessage() ([]byte, error)
	// WriteMessage sends one complete message.
	WriteMessage(data []byte) error
	// Close closes the connection.
	Close() error
}

type session struct {
	mu      sync.Mutex
	agent   Conn
	browser Conn
	ready   chan struct{} // closed when both sides are registered
	started bool
	piping  bool // guards the one-time local pipe start
}

// setSide assigns conn to the side's slot, returning ErrDuplicateSide if that
// side is already registered. Callers must hold s.mu.
func (s *session) setSide(side Side, conn Conn) error {
	switch side {
	case SideAgent:
		if s.agent != nil {
			return ErrDuplicateSide
		}
		s.agent = conn
	case SideBrowser:
		if s.browser != nil {
			return ErrDuplicateSide
		}
		s.browser = conn
	}
	return nil
}

// markStarted records the first registration on the session, incrementing the
// active count exactly once. It returns true only for that first side. Callers
// must hold s.mu.
func (s *session) markStarted(count *atomic.Int64) bool {
	if s.started {
		return false
	}
	s.started = true
	count.Add(1)
	return true
}

// Relay pipes WebSocket connections from browsers and agents together.
type Relay struct {
	sessions sync.Map // map[protocol.SessionToken]*session
	count    atomic.Int64
	logger   *slog.Logger

	// registry tracks session affinity/ownership through the SessionRegistry
	// port. The live Conn pair stays in the sessions map above; the
	// registry only tracks token → owning serverID so a relay pool can route
	// cross-server. With InProcessRegistry this is a single-server shadow.
	registry SessionRegistry
	serverID string

	// registryUnhealthySince holds the UnixNano timestamp of the first failed
	// registry health probe in the current unhealthy streak, or 0 when healthy.
	// Written by MonitorRegistryHealth, read locklessly by Register's degraded
	// gate and the opengate_registry_up gauge (RegistryUp). degradedThreshold is
	// how long that streak must last before Register fails closed.
	registryUnhealthySince atomic.Int64
	degradedThreshold      time.Duration

	// OnSessionEnd is called when a session finishes piping (both sides disconnected).
	// It can be used to clean up external state such as DB sessions.
	OnSessionEnd func(token protocol.SessionToken)
}

// Option configures a Relay at construction.
type Option func(*Relay)

// WithRegistry injects the SessionRegistry adapter and the caller's stable
// serverID. Without it, NewRelay defaults to an in-process registry with
// serverID "local". A future distributed adapter would be injected here.
func WithRegistry(reg SessionRegistry, serverID string) Option {
	return func(r *Relay) {
		r.registry = reg
		r.serverID = serverID
	}
}

// NewRelay creates a new Relay. By default the relay is backed by an in-process
// SessionRegistry so the live path is always registry-driven; pass WithRegistry
// to inject a distributed adapter.
func NewRelay(logger *slog.Logger, opts ...Option) *Relay {
	r := &Relay{
		logger:            logger,
		registry:          NewInProcessRegistry(),
		serverID:          defaultServerID,
		degradedThreshold: DefaultDegradedThreshold,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register registers one side of a session identified by token. When both sides
// are registered, piping starts automatically.
func (r *Relay) Register(ctx context.Context, token protocol.SessionToken, conn Conn, side Side) error {
	// Once the session registry has been unreachable past degradedThreshold,
	// refuse to start a *new* session. A session already in flight (entry present)
	// is unaffected: its second side still pairs and existing traffic continues.
	// InProcessRegistry never reports unhealthy, so single-server deployments
	// never reach here.
	if _, inFlight := r.sessions.Load(token); !inFlight && r.RegistryDegraded() {
		return ErrRegistryDegraded
	}

	val, _ := r.sessions.LoadOrStore(token, &session{
		ready: make(chan struct{}),
	})
	s := val.(*session)

	s.mu.Lock()
	if err := s.setSide(side, conn); err != nil {
		s.mu.Unlock()
		return err
	}
	firstSide := s.markStarted(&r.count)
	s.mu.Unlock()

	// Express the session lifecycle through the SessionRegistry port. With the
	// in-process adapter these calls shadow the live sessions map and never alter
	// routing. Registry failures are logged, not fatal — the live relay remains
	// the source of truth for the in-process Conn pair. token_prefix is redacted
	// inline at each call site so the full token never reaches logs.
	if firstSide {
		r.writeOwnerMeta(ctx, token)
	}

	r.startPipeIfReady(token, s)
	return nil
}

// writeOwnerMeta records the session metadata in the registry. Failures are
// logged, not fatal — the live Conn pair in the sessions map is authoritative.
func (r *Relay) writeOwnerMeta(ctx context.Context, token protocol.SessionToken) {
	if err := r.registry.SaveSession(ctx, token, SessionMeta{
		CreatedAt:     time.Now(),
		ExpectedSides: []Side{SideAgent, SideBrowser},
		ServerID:      r.serverID,
	}); err != nil {
		r.logger.Error("registry save session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
	}
}

// startPipeIfReady starts the local pipe exactly once, when both sides are
// present. Guarded by s.piping so the close of s.ready and the pipe goroutine
// launch happen exactly once even though the lock is released between
// registration phases.
func (r *Relay) startPipeIfReady(token protocol.SessionToken, s *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.piping || s.agent == nil || s.browser == nil {
		return
	}
	s.piping = true
	close(s.ready)
	pipeCtx, cancel := context.WithCancel(context.Background())
	go r.pipe(pipeCtx, cancel, token, s)
}

// WaitForPeer blocks until the peer side of the given token is registered
// or the context is cancelled.
func (r *Relay) WaitForPeer(ctx context.Context, token protocol.SessionToken) error {
	val, ok := r.sessions.Load(token)
	if !ok {
		return ErrSessionNotFound
	}
	s := val.(*session)

	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ActiveSessionCount returns the number of active sessions.
func (r *Relay) ActiveSessionCount() int {
	return int(r.count.Load())
}

// PingRegistry reports whether the relay's SessionRegistry backing store is
// reachable. The readiness probe uses it to drain a pod that has lost its
// distributed store. Always nil with the in-process
// adapter.
func (r *Relay) PingRegistry(ctx context.Context) error {
	return r.registry.Ping(ctx)
}

// copyMessages reads complete messages from src and writes them to dst,
// preserving message boundaries. Returns when either side errors.
func (r *Relay) copyMessages(dst, src Conn, direction string, tp string) {
	var count int
	for {
		data, err := src.ReadMessage()
		if err != nil {
			r.logger.Error("relay read error", "direction", direction, "token_prefix", tp, "msgs_copied", count, "error", err)
			return
		}
		if err := dst.WriteMessage(data); err != nil {
			r.logger.Error("relay write error", "direction", direction, "token_prefix", tp, "msgs_copied", count, "error", err)
			return
		}
		count++
	}
}

// pipe forwards messages between agent and browser until one side disconnects
// or the session context is cancelled.
func (r *Relay) pipe(ctx context.Context, cancel context.CancelFunc, token protocol.SessionToken, s *session) {
	tp := protocol.RedactToken(string(token))
	r.logger.Info("relay session started", "token_prefix", tp)

	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = s.agent.Close()
			_ = s.browser.Close()
		})
	}

	defer func() {
		closeBoth()
		cancel()
		r.sessions.Delete(token)
		r.count.Add(-1)
		// Release the registry entry. Use a background context — the originating
		// request context is long gone.
		if err := r.registry.DeleteSession(context.Background(), token); err != nil {
			r.logger.Error("registry delete session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
		}
		r.logger.Info("relay session ended", "token_prefix", tp)
		if r.OnSessionEnd != nil {
			r.OnSessionEnd(token)
		}
	}()

	done := make(chan struct{})

	// agent → browser
	go func() {
		defer close(done)
		r.copyMessages(s.browser, s.agent, "agent→browser", tp)
	}()

	// browser → agent
	go func() {
		r.copyMessages(s.agent, s.browser, "browser→agent", tp)
		// When this direction ends, close both to unblock the other.
		closeBoth()
	}()

	select {
	case <-done:
	case <-ctx.Done():
		closeBoth()
		<-done
	}
}
