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

func TestRelayFaults_BrowserClosesMidStream(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	for i := 0; i < 3; i++ {
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, []byte{byte(i)}))
		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		require.Equal(t, []byte{byte(i)}, data)
	}

	require.Equal(t, 1, env.relay.ActiveSessionCount())

	// An abnormal closure makes the relay's browser read fail, which shuts the pipe both ways.
	require.NoError(t, browserConn.CloseNow())

	// The agent reads first so the close handshake completes; the relay's agent Close would
	// otherwise block on a peer that never reads.
	readCtx, readCancel := context.WithTimeout(ctx, 5*time.Second)
	defer readCancel()
	_, _, err := agentConn.Read(readCtx)
	assert.Error(t, err, "agent read should surface close after browser disconnect")

	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 3*time.Second, 25*time.Millisecond, "relay should drop session after browser close")
}

func TestRelayFaults_AgentClosesWithBufferedTraffic(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 5*time.Second)
	defer wsCancel()

	const burst = 16
	for i := 0; i < burst; i++ {
		payload := []byte{0xAA, byte(i)}
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
	}
	require.NoError(t, agentConn.CloseNow())

	for {
		drainCtx, drainCancel := context.WithTimeout(ctx, 500*time.Millisecond)
		_, _, err := browserConn.Read(drainCtx)
		drainCancel()
		if err != nil {
			break
		}
	}

	require.Eventually(t, func() bool {
		return env.relay.ActiveSessionCount() == 0
	}, 3*time.Second, 25*time.Millisecond, "relay should drop session after agent close")

	writeCtx, writeCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer writeCancel()
	_ = browserConn.Write(writeCtx, websocket.MessageBinary, []byte("post-close"))
}

func TestRelayFaults_ConcurrentBidirectionalOrdering(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	const perSide = 200

	var wg sync.WaitGroup

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
