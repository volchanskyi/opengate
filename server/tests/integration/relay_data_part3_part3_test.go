package integration

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"nhooyr.io/websocket"
	"testing"
)

func relayEncodedControl(t *testing.T, msg *protocol.ControlMessage) (byte, []byte) {
	t.Helper()
	payload, err := (&protocol.Codec{}).EncodeControl(msg)
	require.NoError(t, err)
	return protocol.FrameControl, payload
}

func sendRelayFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, frameType byte, payload []byte) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, (&protocol.Codec{}).WriteFrame(&buf, frameType, payload))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, buf.Bytes()))
}

func readRelayFrame(t *testing.T, ctx context.Context, conn *websocket.Conn) (byte, []byte) {
	t.Helper()
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	frameType, payload, err := (&protocol.Codec{}).ReadFrame(bytes.NewReader(data))
	require.NoError(t, err)
	return frameType, payload
}

func assertRelayMouseMove(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	frameType, payload := readRelayFrame(t, ctx, conn)
	assert.Equal(t, protocol.FrameControl, frameType)
	msg, err := (&protocol.Codec{}).DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgMouseMove, msg.Type)
}

func assertRelayFileList(t *testing.T, ctx context.Context, conn *websocket.Conn) {
	t.Helper()
	frameType, payload := readRelayFrame(t, ctx, conn)
	assert.Equal(t, protocol.FrameControl, frameType)
	msg, err := (&protocol.Codec{}).DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgFileListResponse, msg.Type)
	assert.Equal(t, "/home", msg.Path)
	require.Len(t, msg.Entries, 1)
	assert.Equal(t, "test.txt", msg.Entries[0].Name)
}
