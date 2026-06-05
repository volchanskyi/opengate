// Package relay implements the WebSocket relay that pipes browser and agent
// connections together.
package relay

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// DefaultAffinityTTL bounds how long a dead owner's affinity claim survives
// before another server may reclaim it. Ignored by InProcessRegistry (single
// server); honored by RedisRegistry (Phase 13b PR-C, ADR-023).
const DefaultAffinityTTL = 30 * time.Second

// defaultServerID is the serverID used when NewRelay is called without
// WithRegistry. Any non-empty stable value satisfies the in-process adapter.
const defaultServerID = "local"

var (
	// ErrDuplicateSide is returned when the same side of a session is registered twice.
	ErrDuplicateSide = errors.New("session side already registered")
	// ErrSessionNotFound is returned when a session token is not found.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionProxied is returned when a second local side registers on a
	// session this server is already proxying to a foreign owner. That side must
	// reconnect (with a fresh token) and proxy independently rather than corrupt
	// the in-flight cross-server splice (Phase 13b PR-C, ADR-033).
	ErrSessionProxied = errors.New("session already proxied to owner")
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

// PeerDialer establishes the cross-server tunnel to a session's affinity owner.
// It is consulted only when a distributed SessionRegistry reports that another
// server owns the session (Phase 13b PR-C, ADR-033); with InProcessRegistry the
// caller always owns its own claim and the dialer is never called. Dial returns
// a Conn carrying the proxied side's messages, framed identically to a local
// relay Conn.
type PeerDialer interface {
	// Dial connects to owner for token's session, presenting the given side as
	// the one being forwarded. It returns the peer Conn or an error.
	Dial(ctx context.Context, owner string, token protocol.SessionToken, side Side) (Conn, error)
}

type session struct {
	mu      sync.Mutex
	agent   Conn
	browser Conn
	ready   chan struct{} // closed when both sides are registered (or the tunnel is up)
	started bool
	piping  bool // guards the one-time local pipe start
	proxied bool // session is spliced to a foreign owner, not paired locally
	done    bool // terminal: a half-open proxied side was torn down before pairing
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
	// port (ADR-023). The live Conn pair stays in the sessions map above; the
	// registry only tracks token → owning serverID so a relay pool can route
	// cross-server. With InProcessRegistry this is a single-server shadow.
	registry    SessionRegistry
	serverID    string
	affinityTTL time.Duration

	// registryUnhealthySince holds the UnixNano timestamp of the first failed
	// registry health probe in the current unhealthy streak, or 0 when healthy.
	// Written by MonitorRegistryHealth, read locklessly by Register's degraded
	// gate and the opengate_registry_up gauge (RegistryUp). degradedThreshold is
	// how long that streak must last before Register fails closed.
	registryUnhealthySince atomic.Int64
	degradedThreshold      time.Duration

	// peerDialer, when set, makes the relay splice foreign-owned sessions across
	// servers instead of pairing them locally (Phase 13b PR-C, ADR-033). Nil in
	// single-server deployments.
	peerDialer PeerDialer

	// OnSessionEnd is called when a session finishes piping (both sides disconnected).
	// It can be used to clean up external state such as DB sessions.
	OnSessionEnd func(token protocol.SessionToken)
}

// Option configures a Relay at construction.
type Option func(*Relay)

// WithRegistry injects the SessionRegistry adapter and the caller's stable
// serverID. Without it, NewRelay defaults to an in-process registry with
// serverID "local". RedisRegistry is swapped in here at Phase 13b (PR-C).
func WithRegistry(reg SessionRegistry, serverID string) Option {
	return func(r *Relay) {
		r.registry = reg
		r.serverID = serverID
	}
}

// WithPeerDialer injects the cross-server PeerDialer. Without it, a session
// owned by another server is logged and handled locally (single-server
// fallback); with it, such sessions are spliced to the owner (Phase 13b PR-C).
func WithPeerDialer(d PeerDialer) Option {
	return func(r *Relay) {
		r.peerDialer = d
	}
}

// WithAffinityTTL overrides the affinity-claim TTL, which also bounds how long
// the owner waits for a proxied side's local peer before tearing a half-open
// session down (Phase 13b PR-C).
func WithAffinityTTL(ttl time.Duration) Option {
	return func(r *Relay) {
		r.affinityTTL = ttl
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
		affinityTTL:       DefaultAffinityTTL,
		degradedThreshold: DefaultDegradedThreshold,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register registers one side of a session identified by token. When both local
// sides are registered, piping starts automatically; when a distributed registry
// reports a foreign owner and a PeerDialer is set, the side is instead spliced to
// that owner across servers (Phase 13b PR-C, ADR-033).
func (r *Relay) Register(ctx context.Context, token protocol.SessionToken, conn Conn, side Side) error {
	// Degraded-mode gate (ADR-023 recovery posture): once the session registry
	// has been unreachable past degradedThreshold, refuse to start a *new* session
	// — its affinity can't be claimed, so a cross-server pair could silently
	// split-brain. A session already in flight (entry present) is unaffected: its
	// second side still pairs and existing traffic continues. InProcessRegistry
	// never reports unhealthy, so single-server deployments never reach here.
	if _, inFlight := r.sessions.Load(token); !inFlight && r.RegistryDegraded() {
		return ErrRegistryDegraded
	}

	val, _ := r.sessions.LoadOrStore(token, &session{
		ready: make(chan struct{}),
	})
	s := val.(*session)

	s.mu.Lock()
	if s.proxied {
		// This server is already proxying the session to its owner; a second
		// local side must reconnect and proxy independently.
		s.mu.Unlock()
		return ErrSessionProxied
	}
	if err := s.setSide(side, conn); err != nil {
		s.mu.Unlock()
		return err
	}
	firstSide := s.markStarted(&r.count)
	// Resolve ownership while holding the lock so a racing second side observes
	// the proxied flag before it can pass the gate above. The actual peer dial
	// happens after the unlock — never under s.mu.
	var owner string
	if firstSide {
		owner = r.resolveOwner(ctx, token)
		if owner != r.serverID && r.peerDialer != nil {
			s.proxied = true
		}
	}
	proxied := s.proxied
	s.mu.Unlock()

	if firstSide && proxied {
		return r.spliceToOwner(ctx, token, s, conn, side, owner)
	}

	// Express the session lifecycle through the SessionRegistry port. With the
	// in-process adapter these calls shadow the live sessions map and never alter
	// routing. Registry failures are logged, not fatal — the live relay remains
	// the source of truth for the in-process Conn pair. token_prefix is redacted
	// inline at each call site (ADR-027 pen-test gate).
	if firstSide {
		if owner != r.serverID {
			// Foreign owner but no PeerDialer (single-server fallback): proceed
			// locally as before.
			r.logger.Warn("session owned by another server", "token_prefix", protocol.RedactToken(string(token)), "owner", owner)
		}
		r.writeOwnerMeta(ctx, token)
	}
	_ = r.registry.PublishEvent(ctx, SessionEvent{Kind: EventSideJoined, Token: token, ServerID: r.serverID, Side: &side})

	r.startPipeIfReady(token, s)
	return nil
}

// RegisterLocal registers a side that arrived over the cross-server proxy, from
// the owner's perspective. Unlike Register it makes no affinity or proxy
// decision: this server already resolved as the owner, and a proxied conn must
// never re-proxy (the loop guard). It pairs with the locally-present peer and
// starts the pipe, waiting up to affinityTTL for that peer. If none appears
// (stale affinity — the owner-side conn is already gone) it tears the half-open
// session down, closes conn, and returns an error so the caller closes the
// tunnel and the client reconnects with a fresh token (ADR-033).
func (r *Relay) RegisterLocal(ctx context.Context, token protocol.SessionToken, conn Conn, side Side) error {
	val, _ := r.sessions.LoadOrStore(token, &session{
		ready: make(chan struct{}),
	})
	s := val.(*session)

	s.mu.Lock()
	if err := s.setSide(side, conn); err != nil {
		s.mu.Unlock()
		return err
	}
	s.markStarted(&r.count)
	s.mu.Unlock()

	r.startPipeIfReady(token, s)

	waitCtx, cancel := context.WithTimeout(ctx, r.affinityTTL)
	defer cancel()
	if err := r.WaitForPeer(waitCtx, token); err != nil {
		r.dropHalfOpen(token, s, conn)
		return fmt.Errorf("await local peer for proxied side: %w", err)
	}
	return nil
}

// dropHalfOpen tears down a proxied side whose local peer never arrived within
// affinityTTL. It is mutually exclusive with startPipeIfReady via s.done/s.piping
// under s.mu, so a peer arriving at the timeout boundary either wins (pipe owns
// teardown) or loses (this drop owns it) — never both.
func (r *Relay) dropHalfOpen(token protocol.SessionToken, s *session, conn Conn) {
	s.mu.Lock()
	if s.piping || s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.mu.Unlock()

	_ = conn.Close()
	if _, loaded := r.sessions.LoadAndDelete(token); loaded {
		r.count.Add(-1)
	}
}

// resolveOwner claims affinity for this server and returns the owning serverID.
// On a claim error it logs and falls back to this server (the live Conn pair
// remains authoritative), so a registry outage never breaks same-server relays.
func (r *Relay) resolveOwner(ctx context.Context, token protocol.SessionToken) string {
	owner, err := r.registry.ClaimAffinity(ctx, token, r.serverID, r.affinityTTL)
	if err != nil {
		r.logger.Error("registry claim affinity", "token_prefix", protocol.RedactToken(string(token)), "error", err)
		return r.serverID
	}
	return owner
}

// writeOwnerMeta records the session metadata and broadcasts EventCreated. Only
// the owning server calls this (proxied sides skip it — the owner owns the meta).
func (r *Relay) writeOwnerMeta(ctx context.Context, token protocol.SessionToken) {
	if err := r.registry.SaveSession(ctx, token, SessionMeta{
		CreatedAt:     time.Now(),
		ExpectedSides: []Side{SideAgent, SideBrowser},
		ServerID:      r.serverID,
	}); err != nil {
		r.logger.Error("registry save session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
	}
	_ = r.registry.PublishEvent(ctx, SessionEvent{Kind: EventCreated, Token: token, ServerID: r.serverID})
}

// startPipeIfReady starts the local pipe exactly once, when both sides are
// present and the session is not proxied. Guarded by s.piping so the close of
// s.ready and the pipe goroutine launch happen exactly once even though the
// lock is released between registration phases.
func (r *Relay) startPipeIfReady(token protocol.SessionToken, s *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proxied || s.piping || s.done || s.agent == nil || s.browser == nil {
		return
	}
	s.piping = true
	close(s.ready)
	pipeCtx, cancel := context.WithCancel(context.Background())
	go r.pipe(pipeCtx, cancel, token, s)
}

// spliceToOwner dials the affinity owner and pipes the local conn through the
// cross-server tunnel. The proxied session never pairs locally and never
// re-proxies (the owner registers the tunnel via RegisterLocal). On dial failure
// it fails fast (ADR-023): close the local conn and drop the session so the
// client reconnects with a fresh token. On success it marks the session ready so
// WaitForPeer unblocks once the tunnel is up.
func (r *Relay) spliceToOwner(ctx context.Context, token protocol.SessionToken, s *session, local Conn, side Side, owner string) error {
	tp := protocol.RedactToken(string(token))
	peer, err := r.peerDialer.Dial(ctx, owner, token, side)
	if err != nil {
		r.logger.Error("relay peer dial", "token_prefix", protocol.RedactToken(string(token)), "owner", owner, "error", err)
		_ = local.Close()
		r.sessions.Delete(token)
		r.count.Add(-1)
		return fmt.Errorf("dial relay owner: %w", err)
	}
	r.logger.Info("relay proxying to owner", "token_prefix", protocol.RedactToken(string(token)), "owner", owner)
	close(s.ready)
	go r.spliceProxied(token, local, peer, tp)
	return nil
}

// spliceProxied bidirectionally copies messages between the local conn and the
// cross-server peer tunnel until either end closes, then tears down the proxied
// session. Unlike pipe(), it performs no registry writes: this server does not
// own the affinity claim (the owner created and will delete it), so it only
// releases the local conn pair and the active-session slot it took.
func (r *Relay) spliceProxied(token protocol.SessionToken, local, peer Conn, tp string) {
	r.logger.Info("relay proxy splice started", "token_prefix", protocol.RedactToken(string(token)))

	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = local.Close()
			_ = peer.Close()
		})
	}

	defer func() {
		closeBoth()
		r.sessions.Delete(token)
		r.count.Add(-1)
		r.logger.Info("relay proxy splice ended", "token_prefix", protocol.RedactToken(string(token)))
		if r.OnSessionEnd != nil {
			r.OnSessionEnd(token)
		}
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		r.copyMessages(peer, local, "local→peer", tp)
	}()
	r.copyMessages(local, peer, "peer→local", tp)
	closeBoth()
	<-done
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
// distributed store (ADR-023 recovery posture). Always nil with the in-process
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
		// Release the affinity claim and notify peers the session ended. Use a
		// background context — the originating request context is long gone.
		if err := r.registry.DeleteSession(context.Background(), token); err != nil {
			r.logger.Error("registry delete session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
		}
		_ = r.registry.PublishEvent(context.Background(), SessionEvent{Kind: EventEnded, Token: token, ServerID: r.serverID})
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
