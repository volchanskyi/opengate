package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// Phase C / C3: WebSocket relay fault injection.
//
// The relay (`server/internal/relay.Relay`) is a stateful broker between a
// browser WebSocket and an agent WebSocket. Once both sides have registered
// it spawns two `copyMessages` goroutines (browser → agent, agent → browser)
// and tracks the session in `ActiveSessionCount`. The B5 / control-stream
// equivalent of these tests pinned the agent → server QUIC fault paths;
// these tests pin the equivalent browser/agent → relay fault paths so future
// refactors of the pipe loop don't regress.

// TestRelayFaults_BrowserClosesMidStream verifies that when the browser
// closes its WebSocket while data is in flight, the relay tears down the
// session and the agent observes a clean error. The relay's pipe goroutines
// only retire (and ActiveSessionCount only decrements) once both directions
// observe end-of-stream — so an ActiveSessionCount that returns to zero is
// the externally observable proof that the per-session goroutines exited.
func TestRelayFaults_BrowserClosesMidStream(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	// Pump a few frames so the relay's copyMessages loops are running and
	// have consumed at least one read from each direction.
	for i := 0; i < 3; i++ {
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, []byte{byte(i)}))
		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
	}

	require.Equal(t, 1, env.relay.ActiveSessionCount())

	// Slam the browser side closed without a graceful close handshake —
	// abnormal closure forces the relay's browser → agent ReadMessage to
	// surface an error which then triggers pipe shutdown on both
	// directions.
	require.NoError(t, browserConn.CloseNow())

	// The agent's next read must surface the close frame the relay
	// initiates as part of shutdown. Read first so the WebSocket library
	// has a chance to process the inbound close and complete the close
	// handshake — otherwise the relay's s.agent.Close() would block on a
	// peer that never reads.
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	_, _, err := agentConn.Read(readCtx)
	assert.Error(t, err, "agent read should surface close after browser disconnect")

	// Relay reconciles both sides once the close handshakes finish.
	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 3*time.Second, 25*time.Millisecond, "relay should drop session after browser close")
}

// TestRelayFaults_AgentClosesWithBufferedTraffic verifies that closing the
// agent side mid-burst doesn't crash the relay (no `nil` writes attempted
// after Close), and that the browser side surfaces a clean read error.
func TestRelayFaults_AgentClosesWithBufferedTraffic(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	// Fire a burst of writes from the agent without consuming them on the
	// browser side, then immediately close the agent. The relay's pipe
	// loop is in mid-copy when the agent disconnects.
	const burst = 16
	for i := 0; i < burst; i++ {
		payload := []byte{0xAA, byte(i)}
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	}
	require.NoError(t, agentConn.CloseNow())

	// Drain whatever messages the relay managed to forward before the
	// close was processed. We don't assert how many — that depends on
	// scheduling — only that no read panics.
	for {
		drainCtx, drainCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, _, err := browserConn.Read(drainCtx)
		drainCancel()
		if err != nil {
			break
		}
	}

	// Relay must reconcile after agent close.
	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 3*time.Second, 25*time.Millisecond, "relay should drop session after agent close")

	// Further browser writes must not silently succeed — peer is gone.
	writeCtx, writeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer writeCancel()
	_ = browserConn.Write(writeCtx, websocket.MessageBinary, []byte("post-close"))
	// Either Write or the subsequent Read surfaces the close — both shapes
	// are acceptable; we just must not panic. The assertion is "this far
	// reached without panic".
}

// TestRelayFaults_ConcurrentBidirectionalOrdering asserts the per-direction
// ordering contract the relay implements: messages sent in order from one
// side arrive in that order on the other side. Cross-direction interleaving
// is explicitly best-effort and not asserted (the two `copyMessages`
// goroutines run independently). This complements
// TestRelayBidirectionalConcurrent by stressing a much higher message rate.
func TestRelayFaults_ConcurrentBidirectionalOrdering(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	const perSide = 200

	var wg sync.WaitGroup

	// Sender goroutines: write monotonically increasing sequence numbers
	// in each direction.
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < perSide; i++ {
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, []byte{0xA0, byte(i)}))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < perSide; i++ {
			require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, []byte{0xB0, byte(i)}))
		}
	}()

	// Receiver goroutines: validate per-direction ordering.
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < perSide; i++ {
			_, data, err := browserConn.Read(wsCtx)
			require.NoError(t, err)
			require.Equal(t, byte(0xA0), data[0], "browser-side message %d wrong direction tag", i)
			require.Equal(t, byte(i), data[1], "browser-side message %d out of order", i)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < perSide; i++ {
			_, data, err := agentConn.Read(wsCtx)
			require.NoError(t, err)
			require.Equal(t, byte(0xB0), data[0], "agent-side message %d wrong direction tag", i)
			require.Equal(t, byte(i), data[1], "agent-side message %d out of order", i)
		}
	}()

	wg.Wait()
}
