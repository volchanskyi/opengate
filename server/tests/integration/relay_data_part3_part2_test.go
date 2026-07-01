package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"sync"
	"testing"
	"time"
)

func TestRelayProtocolTerminalFrameRoundTrip(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()
	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	codec := &protocol.Codec{}
	payload, err := codec.EncodeTerminalFrame(&protocol.TerminalFrame{Data: []byte("ls -la\n")})
	require.NoError(t, err)
	sendRelayFrame(t, wsCtx, agentConn, protocol.FrameTerminal, payload)
	recvFT, recvPayload := readRelayFrame(t, wsCtx, browserConn)

	assert.Equal(t, protocol.FrameTerminal, recvFT)
	tf, err := codec.DecodeTerminalFrame(recvPayload)
	require.NoError(t, err)
	assert.Equal(t, []byte("ls -la\n"), tf.Data)
}

func TestRelayProtocolBidirectionalControl(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()
	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	mouseFT, mousePayload := relayEncodedControl(t, &protocol.ControlMessage{Type: protocol.MsgMouseMove, X: 50, Y: 75})
	fileFT, filePayload := relayEncodedControl(t, &protocol.ControlMessage{
		Type:    protocol.MsgFileListResponse,
		Path:    "/home",
		Entries: []protocol.FileEntry{{Name: "test.txt", IsDir: false, Size: 42, Modified: 1700000000}},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		sendRelayFrame(t, wsCtx, browserConn, mouseFT, mousePayload)
	}()
	go func() {
		defer wg.Done()
		sendRelayFrame(t, wsCtx, agentConn, fileFT, filePayload)
	}()
	wg.Wait()

	assertRelayMouseMove(t, wsCtx, agentConn)
	assertRelayFileList(t, wsCtx, browserConn)
}
