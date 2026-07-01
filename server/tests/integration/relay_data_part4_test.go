package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
	"sync"
	"testing"
	"time"
)

func TestRelayBidirectionalConcurrent(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	const msgCount = 20

	var wg sync.WaitGroup

	// Agent → Browser
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			payload := []byte{byte(i), 'A'}
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))
		}
	}()

	// Browser → Agent
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			payload := []byte{byte(i), 'B'}
			require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, payload))
		}
	}()

	// Receive at browser (from agent)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			_, data, err := browserConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, byte('A'), data[1], "expected agent message at browser")
		}
	}()

	// Receive at agent (from browser)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < msgCount; i++ {
			_, data, err := agentConn.Read(wsCtx)
			require.NoError(t, err)
			assert.Equal(t, byte('B'), data[1], "expected browser message at agent")
		}
	}()

	wg.Wait()
}
