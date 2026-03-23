// Package relay implements the WebSocket relay that pipes browser and agent
// connections together.
package relay

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

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
}

// Relay pipes WebSocket connections from browsers and agents together.
type Relay struct {
	sessions sync.Map // map[protocol.SessionToken]*session
	count    atomic.Int64
	// OnSessionEnd is called when a session finishes piping (both sides disconnected).
	// It can be used to clean up external state such as DB sessions.
	OnSessionEnd func(token protocol.SessionToken)
}

// NewRelay creates a new Relay.
func NewRelay() *Relay {
	return &Relay{}
}

// Register registers one side of a session identified by token.
// When both sides are registered, piping starts automatically.
func (r *Relay) Register(ctx context.Context, token protocol.SessionToken, conn Conn, side Side) error {
	val, _ := r.sessions.LoadOrStore(token, &session{
		ready: make(chan struct{}),
	})
	s := val.(*session)

	s.mu.Lock()
	defer s.mu.Unlock()

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

	// If this is the first side, increment count.
	if !s.started {
		s.started = true
		r.count.Add(1)
	}

	// If both sides are now present, start piping.
	if s.agent != nil && s.browser != nil {
		close(s.ready)
		pipeCtx, cancel := context.WithCancel(ctx)
		go r.pipe(pipeCtx, cancel, token, s)
	}

	return nil
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

// copyMessages reads complete messages from src and writes them to dst,
// preserving message boundaries. Returns when either side errors.
func copyMessages(dst, src Conn) {
	for {
		data, err := src.ReadMessage()
		if err != nil {
			return
		}
		if err := dst.WriteMessage(data); err != nil {
			return
		}
	}
}

// pipe forwards messages between agent and browser until one side disconnects
// or the session context is cancelled.
func (r *Relay) pipe(ctx context.Context, cancel context.CancelFunc, token protocol.SessionToken, s *session) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			s.agent.Close()
			s.browser.Close()
		})
	}

	defer func() {
		closeBoth()
		cancel()
		r.sessions.Delete(token)
		r.count.Add(-1)
		if r.OnSessionEnd != nil {
			r.OnSessionEnd(token)
		}
	}()

	done := make(chan struct{})

	// agent → browser
	go func() {
		defer close(done)
		copyMessages(s.browser, s.agent)
	}()

	// browser → agent
	go func() {
		copyMessages(s.agent, s.browser)
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
