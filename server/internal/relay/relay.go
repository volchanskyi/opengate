// Package relay implements the WebSocket relay that pipes browser and agent
// connections together.
package relay

import (
	"context"
	"errors"
	"io"
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

// Conn is the interface used by the relay to read, write, and close a
// connection. *websocket.Conn satisfies this; tests inject net.Pipe pairs.
type Conn interface {
	io.ReadWriteCloser
}

type session struct {
	mu      sync.Mutex
	agent   Conn
	browser Conn
	ready   chan struct{} // closed when both sides are registered
	done    <-chan struct{}
	cancel  context.CancelFunc
}

// Relay pipes WebSocket connections from browsers and agents together.
type Relay struct {
	sessions sync.Map // map[protocol.SessionToken]*session
	count    atomic.Int64
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

	// If this is the first side, create a lifecycle context and increment count.
	if s.done == nil {
		ctx, cancel := context.WithCancel(context.Background())
		s.done = ctx.Done()
		s.cancel = cancel
		r.count.Add(1)
	}

	// If both sides are now present, start piping.
	if s.agent != nil && s.browser != nil {
		close(s.ready)
		go r.pipe(token, s)
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

// pipe copies data between agent and browser until one side disconnects
// or the session context is cancelled.
func (r *Relay) pipe(token protocol.SessionToken, s *session) {
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			s.agent.Close()
			s.browser.Close()
		})
	}

	defer func() {
		closeBoth()
		s.cancel()
		r.sessions.Delete(token)
		r.count.Add(-1)
	}()

	done := make(chan struct{})

	// agent → browser
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		io.CopyBuffer(s.browser, s.agent, buf)
	}()

	// browser → agent
	go func() {
		buf := make([]byte, 32*1024)
		io.CopyBuffer(s.agent, s.browser, buf)
		// When this direction ends, close both to unblock the other.
		closeBoth()
	}()

	select {
	case <-done:
	case <-s.done:
		closeBoth()
		<-done
	}
}
