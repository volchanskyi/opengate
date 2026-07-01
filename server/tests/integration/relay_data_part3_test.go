package integration

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"testing"
	"time"
)

func TestRelayProtocolControlFrameRoundTrip(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()
	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msg := &protocol.ControlMessage{Type: protocol.MsgMouseMove, X: 100, Y: 200}
	ft, payload := relayEncodedControl(t, msg)
	sendRelayFrame(t, wsCtx, browserConn, ft, payload)
	recvFT, recvPayload := readRelayFrame(t, wsCtx, agentConn)

	assert.Equal(t, protocol.FrameControl, recvFT)
	got, err := (&protocol.Codec{}).DecodeControl(recvPayload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgMouseMove, got.Type)
	assert.Equal(t, uint16(100), got.X)
	assert.Equal(t, uint16(200), got.Y)
}

func TestRelayProtocolFileListFrameRoundTrip(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()
	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	msg := &protocol.ControlMessage{Type: protocol.MsgFileListRequest, Path: "/home"}
	ft, payload := relayEncodedControl(t, msg)
	sendRelayFrame(t, wsCtx, browserConn, ft, payload)
	recvFT, recvPayload := readRelayFrame(t, wsCtx, agentConn)

	assert.Equal(t, protocol.FrameControl, recvFT)
	got, err := (&protocol.Codec{}).DecodeControl(recvPayload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgFileListRequest, got.Type)
	assert.Equal(t, "/home", got.Path)
}
