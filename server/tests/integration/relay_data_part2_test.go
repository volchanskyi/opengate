package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
	"testing"
	"time"
)

func TestRelayLargeFrameSequence(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer wsCancel()

	// Send 20 sequential small messages, verify ordering is preserved
	const msgCount = 20

	for i := 0; i < msgCount; i++ {
		payload := []byte{byte(i), byte(i + 100)}
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, payload))

		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		assert.Equal(t, payload, data, "message %d corrupted or reordered", i)
	}
}
