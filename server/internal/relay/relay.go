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

// defaultServerID is the serverID used when NewRelay is called without WithRegistry.
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

// Conn is a message-oriented connection: each ReadMessage and WriteMessage
// handles exactly one complete message.
type Conn interface {
	// ReadMessage blocks until one complete message is available or an error occurs.
	ReadMessage() ([]byte, error)
	// WriteMessage sends one complete message.
	WriteMessage(data []byte) error
	// Close closes the connection.
	Close() error
}

type session struct {
	mu       sync.Mutex
	agent    Conn
	browser  Conn
	ready    chan struct{}
	done     chan struct{}
	endOnce  sync.Once // both teardown owners call end; only one close happens
	started  bool
	piping   bool
	released bool // Unregister claimed the session; no pipe may start on it
}

func newSession() *session {
	return &session{
		ready: make(chan struct{}),
		done:  make(chan struct{}),
	}
}

// end announces that the session is over; the pipe and Unregister may both call it.
func (s *session) end() {
	s.endOnce.Do(func() { close(s.done) })
}

// setSide assigns conn to the side's slot or returns ErrDuplicateSide. Callers hold s.mu.
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

// markStarted counts the session once, on its first registration, and returns true for it.
// Callers hold s.mu.
func (s *session) markStarted(count *atomic.Int64, total *atomic.Uint64) bool {
	if s.started {
		return false
	}
	s.started = true
	count.Add(1)
	total.Add(1)
	return true
}

// Relay pipes WebSocket connections from browsers and agents together.
type Relay struct {
	sessions sync.Map
	count    atomic.Int64
	// started counts every session ever opened and never falls.
	started atomic.Uint64
	logger  *slog.Logger

	registry SessionRegistry
	serverID string

	// OnSessionEnd is called when a session finishes piping, to clean up external state.
	OnSessionEnd func(token protocol.SessionToken)
}

// Option configures a Relay at construction.
type Option func(*Relay)

// WithRegistry injects the SessionRegistry and the caller's stable serverID.
func WithRegistry(reg SessionRegistry, serverID string) Option {
	return func(r *Relay) {
		r.registry = reg
		r.serverID = serverID
	}
}

// NewRelay creates a Relay backed by an in-process SessionRegistry unless WithRegistry is passed.
func NewRelay(logger *slog.Logger, opts ...Option) *Relay {
	r := &Relay{
		logger:   logger,
		registry: NewInProcessRegistry(),
		serverID: defaultServerID,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Register registers one side of a session and starts piping once both sides are present.
// It returns the done channel because teardown deletes the token a later lookup would use.
func (r *Relay) Register(ctx context.Context, token protocol.SessionToken, conn Conn, side Side) (<-chan struct{}, error) {
	val, _ := r.sessions.LoadOrStore(token, newSession())
	s := val.(*session)

	s.mu.Lock()
	if err := s.setSide(side, conn); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	firstSide := s.markStarted(&r.count, &r.started)
	s.mu.Unlock()

	// Registry failures are logged and the sessions map stays authoritative for routing.
	if firstSide {
		r.writeOwnerMeta(ctx, token)
	}

	r.startPipeIfReady(token, s)
	return s.done, nil
}

// writeOwnerMeta records the session metadata in the registry and logs a failure.
func (r *Relay) writeOwnerMeta(ctx context.Context, token protocol.SessionToken) {
	if err := r.registry.SaveSession(ctx, token, SessionMeta{
		CreatedAt:     time.Now(),
		ExpectedSides: []Side{SideAgent, SideBrowser},
		ServerID:      r.serverID,
	}); err != nil {
		r.logger.Error("registry save session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
	}
}

// startPipeIfReady starts the pipe once, when both sides are present, guarded by s.piping.
// A session Unregister has released stays dead.
func (r *Relay) startPipeIfReady(token protocol.SessionToken, s *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.piping || s.released || s.agent == nil || s.browser == nil {
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

// SessionsStarted returns how many sessions this process has opened, ended ones included.
func (r *Relay) SessionsStarted() uint64 {
	return r.started.Load()
}

// drainPoll is how often WaitForDrain re-reads the active count.
const drainPoll = 20 * time.Millisecond

// WaitForDrain blocks until no session is live, or returns ctx's error when ctx expires first.
// Hijacked connections are untracked by net/http, so Server.Shutdown cannot report them.
func (r *Relay) WaitForDrain(ctx context.Context) error {
	ticker := time.NewTicker(drainPoll)
	defer ticker.Stop()
	for {
		if r.ActiveSessionCount() == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ActiveTokens returns the tokens the relay holds, paired or waiting; the stale-session sweep
// must not collect them.
func (r *Relay) ActiveTokens() []protocol.SessionToken {
	var tokens []protocol.SessionToken
	r.sessions.Range(func(key, _ any) bool {
		tokens = append(tokens, key.(protocol.SessionToken))
		return true
	})
	return tokens
}

// Unregister releases a session that never paired: it drops the entry, count and
// registry record, closes waiting conns and fires OnSessionEnd. Other tokens are no-ops.
func (r *Relay) Unregister(token protocol.SessionToken) {
	val, ok := r.sessions.Load(token)
	if !ok {
		return
	}
	s := val.(*session)

	s.mu.Lock()
	if s.piping || s.released {
		s.mu.Unlock()
		return
	}
	// Set under the lock so a concurrent registration cannot start a pipe on this session.
	s.released = true
	started, agent, browser := s.started, s.agent, s.browser
	s.mu.Unlock()

	r.sessions.CompareAndDelete(token, val)
	if started {
		r.count.Add(-1)
	}
	if err := r.registry.DeleteSession(context.Background(), token); err != nil {
		r.logger.Error("registry delete session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
	}
	r.logger.Info("relay session released unpaired", "token_prefix", protocol.RedactToken(string(token)))
	if r.OnSessionEnd != nil {
		r.OnSessionEnd(token)
	}
	// A graceful WebSocket close can wait for a peer acknowledgement, so cleanup precedes it.
	for _, conn := range []Conn{agent, browser} {
		if conn != nil {
			_ = conn.Close()
		}
	}
	// Runs last so a parked handler unblocks after the graceful close.
	s.end()
}

// copyMessages copies whole messages from src to dst until either side errors.
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
			// Each graceful close waits on an acknowledgement, so they run concurrently.
			var closing sync.WaitGroup
			for _, conn := range []Conn{s.agent, s.browser} {
				closing.Add(1)
				go func(c Conn) {
					defer closing.Done()
					_ = c.Close()
				}(conn)
			}
			closing.Wait()
		})
	}

	// Both directions are waited for so no copier goroutine outlives the session.
	agentToBrowser := make(chan struct{})
	browserToAgent := make(chan struct{})

	defer func() {
		// Bookkeeping precedes the network close, which can wait on an absent peer.
		cancel()
		r.sessions.Delete(token)
		r.count.Add(-1)
		// A background context is used because the originating request context has ended.
		if err := r.registry.DeleteSession(context.Background(), token); err != nil {
			r.logger.Error("registry delete session", "token_prefix", protocol.RedactToken(string(token)), "error", err)
		}
		r.logger.Info("relay session ended", "token_prefix", tp)
		if r.OnSessionEnd != nil {
			r.OnSessionEnd(token)
		}

		closeBoth()
		<-agentToBrowser
		<-browserToAgent
		// Runs last so parked handlers unblock after the graceful close.
		s.end()
	}()

	go func() {
		defer close(agentToBrowser)
		r.copyMessages(s.browser, s.agent, "agent→browser", tp)
	}()

	go func() {
		defer close(browserToAgent)
		r.copyMessages(s.agent, s.browser, "browser→agent", tp)
		closeBoth()
	}()

	select {
	case <-agentToBrowser:
	case <-browserToAgent:
	case <-ctx.Done():
	}
}
