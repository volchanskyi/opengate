package relay

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Teardown: who ends a session, when the two sides are told, and what a
// shutting-down process is able to wait for. The connection-carrying tests live
// beside the pipe in relay_test.go; these are about the session's end.

// TestRelay_Unregister_TearsDownHalfOpenSession covers the side that connects
// and leaves while its peer never arrives: no pipe ever runs, so teardown has
// to come from Unregister — otherwise the entry, the active count and the
// caller's session row all leak for the life of the process.
func TestRelay_Unregister_TearsDownHalfOpenSession(t *testing.T) {
	r := NewRelay(slog.Default())
	var ended []protocol.SessionToken
	r.OnSessionEnd = func(token protocol.SessionToken) { ended = append(ended, token) }

	token := protocol.GenerateSessionToken()
	_, browserRelay := newMockConnPair(t)
	mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	require.Equal(t, 1, r.ActiveSessionCount())

	r.Unregister(token)

	assert.Equal(t, 0, r.ActiveSessionCount())
	assert.Empty(t, r.ActiveTokens())
	assert.Equal(t, []protocol.SessionToken{token}, ended)
}

// TestRelay_Unregister_LeavesPipedSessionAlone keeps Unregister off a paired
// session: the pipe owns that teardown, and a second one would double-count.
func TestRelay_Unregister_LeavesPipedSessionAlone(t *testing.T) {
	r := NewRelay(slog.Default())
	// The pipe goroutine fires OnSessionEnd, so the count is read across
	// goroutines and has to be atomic.
	var ends atomic.Int64
	r.OnSessionEnd = func(protocol.SessionToken) { ends.Add(1) }

	token, agentLocal, browserLocal := registerSession(t, r)
	r.Unregister(token)

	assert.Equal(t, 1, r.ActiveSessionCount())
	assert.Equal(t, int64(0), ends.Load())

	// The pipe still carries frames, then ends the session exactly once.
	require.NoError(t, agentLocal.WriteMessage([]byte("still piping")))
	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, []byte("still piping"), data)

	agentLocal.Close()
	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0 && ends.Load() == 1
	}, time.Second, 10*time.Millisecond)
}

// TestRelay_Unregister_UnknownTokenIsNoop keeps a late or repeated call inert,
// so it can be deferred unconditionally by the WebSocket handler.
func TestRelay_Unregister_UnknownTokenIsNoop(t *testing.T) {
	r := NewRelay(slog.Default())
	var ends int
	r.OnSessionEnd = func(protocol.SessionToken) { ends++ }

	r.Unregister(protocol.GenerateSessionToken())

	assert.Equal(t, 0, r.ActiveSessionCount())
	assert.Equal(t, 0, ends)
}

// TestRelay_Register_ReturnsChannelClosedWhenSessionEnds pins what the handler
// parks on. A completed session must announce itself on the channel handed to
// the side that registered.
func TestRelay_Register_ReturnsChannelClosedWhenSessionEnds(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	agentDone := mustRegister(t, r, context.Background(), token, agentRelay, SideAgent)
	browserDone := mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	awaitPumping(t, agentLocal, browserLocal)

	select {
	case <-agentDone:
		t.Fatal("a live session must not report itself ended")
	default:
	}

	agentLocal.Close()

	for name, done := range map[string]<-chan struct{}{"agent": agentDone, "browser": browserDone} {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatalf("the %s side was never told its session ended", name)
		}
	}
}

// TestRelay_Unregister_EndsTheSession covers the other teardown owner: a side
// whose peer never arrived is released by Unregister, and that release is what
// has to announce the end.
func TestRelay_Unregister_EndsTheSession(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	_, browserRelay := newMockConnPair(t)

	done := mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	r.Unregister(token)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("releasing an unpaired session must announce that it ended")
	}
}

// TestRelay_WaitForDrain covers what a shutting-down process can know.
// Server.Shutdown cannot see a relay session — websocket.Accept hijacked the
// connection, which untracks it — so the relay's own count is the only answer,
// and a caller that runs out of budget gets told rather than blocked.
func TestRelay_WaitForDrain(t *testing.T) {
	t.Run("returns once the last session is booked out", func(t *testing.T) {
		r, agentLocal, browserLocal := readyRelay(t)
		awaitPumping(t, agentLocal, browserLocal)
		require.Equal(t, 1, r.ActiveSessionCount())

		agentLocal.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, r.WaitForDrain(ctx))
		assert.Equal(t, 0, r.ActiveSessionCount())
	})

	t.Run("reports the sessions it could not wait out", func(t *testing.T) {
		r, _, _ := readyRelay(t)
		require.Equal(t, 1, r.ActiveSessionCount())

		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		err := r.WaitForDrain(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("returns immediately when nothing is live", func(t *testing.T) {
		r := NewRelay(slog.Default())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		assert.NoError(t, r.WaitForDrain(ctx), "an already-empty relay is drained whatever the budget")
	})
}

// slowCloseConn is a Conn whose Close blocks until it is released, standing in
// for the graceful WebSocket close that waits on a peer acknowledgement an
// absent peer never sends.
type slowCloseConn struct {
	*mockConn
	entered  chan struct{}
	release  chan struct{}
	inFlight *atomic.Int64
	peak     *atomic.Int64
}

func newSlowCloseConn(inner *mockConn, release chan struct{}, inFlight, peak *atomic.Int64) *slowCloseConn {
	return &slowCloseConn{
		mockConn: inner,
		entered:  make(chan struct{}, 1),
		release:  release,
		inFlight: inFlight,
		peak:     peak,
	}
}

// Close records that it is in flight, blocks until released, and only then
// unblocks the reads the way a real graceful close does.
func (c *slowCloseConn) Close() error {
	select {
	case c.entered <- struct{}{}:
	default:
	}
	now := c.inFlight.Add(1)
	for {
		peak := c.peak.Load()
		if now <= peak || c.peak.CompareAndSwap(peak, now) {
			break
		}
	}
	<-c.release
	c.inFlight.Add(-1)
	return c.mockConn.Close()
}

// TestRelay_Pipe_BooksOutBeforeItWaitsOnTheNetwork holds the teardown order.
//
// A graceful close waits on an acknowledgement, and for as long as it does a
// finished session must not still be counted, still be named by ActiveTokens —
// which the stale-session sweep reads as "in use", so the row cannot be
// collected — or still sit in the registry. Unregister already does its
// bookkeeping before its close and says why in a comment; this is the pipe held
// to the same order.
func TestRelay_Pipe_BooksOutBeforeItWaitsOnTheNetwork(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()

	release := make(chan struct{})
	var inFlight, peak atomic.Int64
	agentLocal, agentRelayInner := newMockConnPair(t)
	browserLocal, browserRelayInner := newMockConnPair(t)
	agentRelay := newSlowCloseConn(agentRelayInner, release, &inFlight, &peak)
	browserRelay := newSlowCloseConn(browserRelayInner, release, &inFlight, &peak)

	mustRegister(t, r, context.Background(), token, agentRelay, SideAgent)
	mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	awaitPumping(t, agentLocal, browserLocal)
	require.Equal(t, 1, r.ActiveSessionCount())

	ended := make(chan struct{})
	r.OnSessionEnd = func(protocol.SessionToken) { close(ended) }

	agentLocal.Close()

	// The books are expected to be closed while the close handshake is still
	// blocked, so this waits for the close to be entered and asserts on the
	// bookkeeping without releasing it.
	select {
	case <-agentRelay.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("teardown never reached the close")
	}

	assert.Equal(t, 0, r.ActiveSessionCount(), "a finished session must not be counted while its close waits")
	assert.Empty(t, r.ActiveTokens(), "a finished session's token must not read as live while its close waits")
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("OnSessionEnd must fire before the close handshake, not after it")
	}

	// Both sides must be closing at once. Sequential closes double the worst
	// case for no gain: neither peer is going to answer.
	require.Eventually(t, func() bool { return peak.Load() == 2 },
		3*time.Second, 10*time.Millisecond, "the two sides must be closed concurrently, not one after the other")

	close(release)
	require.Eventually(t, func() bool { return inFlight.Load() == 0 },
		3*time.Second, 10*time.Millisecond, "both closes must complete")
	_ = browserLocal
}
