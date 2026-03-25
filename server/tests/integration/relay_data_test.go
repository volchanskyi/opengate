package integration

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"nhooyr.io/websocket"
)

// setupRelayPair creates a session and connects both agent and browser WebSockets.
// The returned connections are cleaned up when the test ends.
func (e *sessionTestEnv) setupRelayPair(t *testing.T, ctx context.Context) (agentConn, browserConn *websocket.Conn) {
	t.Helper()

	user := testutil.SeedUser(t, ctx, e.store)
	group := testutil.SeedGroup(t, ctx, e.store, user.ID)

	jwtToken, err := e.jwt.GenerateToken(user.ID, user.Email, user.IsAdmin)
	require.NoError(t, err)

	stream, deviceID := e.connectAgent(t, group.ID)

	require.Eventually(t, func() bool {
		d, err := e.store.GetDevice(ctx, deviceID)
		return err == nil && d.Status == db.StatusOnline
	}, 3*time.Second, 50*time.Millisecond)

	result := e.createSession(t, jwtToken, deviceID, map[string]bool{"desktop": true})

	// Read SessionRequest and accept
	codec := &protocol.Codec{}
	_, _, err = codec.ReadFrame(stream)
	require.NoError(t, err)

	acceptMsg := &protocol.ControlMessage{
		Type:  protocol.MsgSessionAccept,
		Token: protocol.SessionToken(result.Token),
	}
	acceptPayload, err := codec.EncodeControl(acceptMsg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(stream, protocol.FrameControl, acceptPayload))

	agentConn = e.dialRelayWS(t, ctx, result.Token, "agent", "")
	t.Cleanup(func() { agentConn.Close(websocket.StatusNormalClosure, "") })

	browserConn = e.dialRelayWS(t, ctx, result.Token, "browser", jwtToken)
	t.Cleanup(func() { browserConn.Close(websocket.StatusNormalClosure, "") })

	// Wait for relay to wire both sides
	time.Sleep(200 * time.Millisecond)

	return agentConn, browserConn
}

func TestRelayBinaryPayloadIntegrity(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	// Send multiple distinct messages and verify each arrives intact.
	// The relay streams data so messages may be split/merged; we verify
	// by sending individually and reading each message back.
	payloads := [][]byte{
		[]byte("hello-from-agent"),
		make([]byte, 1024),   // 1 KB zeros
		make([]byte, 16*1024), // 16 KB zeros
	}
	// Fill with recognizable patterns
	for i := range payloads[1] {
		payloads[1][i] = byte(i % 256)
	}
	for i := range payloads[2] {
		payloads[2][i] = byte(i % 256)
	}

	for _, p := range payloads {
		require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, p))
		_, data, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		assert.Equal(t, p, data, "payload of size %d corrupted", len(p))
	}
}

func TestRelayLargeFrameSequence(t *testing.T) {
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

func TestRelayProtocolFrameRoundTrip(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

	tests := []struct {
		name      string
		direction string // "browser_to_agent" or "agent_to_browser"
		frameType byte
		encode    func() (byte, []byte, error)
		verify    func(t *testing.T, frameType byte, payload []byte)
	}{
		{
			name:      "control_mouse_move",
			direction: "browser_to_agent",
			encode: func() (byte, []byte, error) {
				msg := &protocol.ControlMessage{Type: protocol.MsgMouseMove, X: 100, Y: 200}
				payload, err := codec.EncodeControl(msg)
				return protocol.FrameControl, payload, err
			},
			verify: func(t *testing.T, ft byte, payload []byte) {
				assert.Equal(t, protocol.FrameControl, ft)
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err)
				assert.Equal(t, protocol.MsgMouseMove, msg.Type)
				assert.Equal(t, uint16(100), msg.X)
				assert.Equal(t, uint16(200), msg.Y)
			},
		},
		{
			name:      "control_file_list_request",
			direction: "browser_to_agent",
			encode: func() (byte, []byte, error) {
				msg := &protocol.ControlMessage{Type: protocol.MsgFileListRequest, Path: "/home"}
				payload, err := codec.EncodeControl(msg)
				return protocol.FrameControl, payload, err
			},
			verify: func(t *testing.T, ft byte, payload []byte) {
				assert.Equal(t, protocol.FrameControl, ft)
				msg, err := codec.DecodeControl(payload)
				require.NoError(t, err)
				assert.Equal(t, protocol.MsgFileListRequest, msg.Type)
				assert.Equal(t, "/home", msg.Path)
			},
		},
		{
			name:      "terminal_frame",
			direction: "agent_to_browser",
			encode: func() (byte, []byte, error) {
				tf := &protocol.TerminalFrame{Data: []byte("ls -la\n")}
				payload, err := codec.EncodeTerminalFrame(tf)
				return protocol.FrameTerminal, payload, err
			},
			verify: func(t *testing.T, ft byte, payload []byte) {
				assert.Equal(t, protocol.FrameTerminal, ft)
				tf, err := codec.DecodeTerminalFrame(payload)
				require.NoError(t, err)
				assert.Equal(t, []byte("ls -la\n"), tf.Data)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft, payload, err := tc.encode()
			require.NoError(t, err)

			// Build wire frame
			var buf bytes.Buffer
			require.NoError(t, codec.WriteFrame(&buf, ft, payload))

			var sender, receiver *websocket.Conn
			if tc.direction == "browser_to_agent" {
				sender, receiver = browserConn, agentConn
			} else {
				sender, receiver = agentConn, browserConn
			}

			require.NoError(t, sender.Write(wsCtx, websocket.MessageBinary, buf.Bytes()))

			_, data, err := receiver.Read(wsCtx)
			require.NoError(t, err)

			// Decode received frame
			recvCodec := &protocol.Codec{}
			recvFT, recvPayload, err := recvCodec.ReadFrame(bytes.NewReader(data))
			require.NoError(t, err)

			tc.verify(t, recvFT, recvPayload)
		})
	}

	// Bidirectional sub-test: simultaneous control messages in both directions
	t.Run("bidirectional_control", func(t *testing.T) {
		// Browser → Agent: MouseMove
		mouseMsg := &protocol.ControlMessage{Type: protocol.MsgMouseMove, X: 50, Y: 75}
		mousePayload, err := codec.EncodeControl(mouseMsg)
		require.NoError(t, err)
		var mouseBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&mouseBuf, protocol.FrameControl, mousePayload))

		// Agent → Browser: FileListResponse
		fileMsg := &protocol.ControlMessage{
			Type: protocol.MsgFileListResponse,
			Path: "/home",
			Entries: []protocol.FileEntry{
				{Name: "test.txt", IsDir: false, Size: 42, Modified: 1700000000},
			},
		}
		filePayload, err := codec.EncodeControl(fileMsg)
		require.NoError(t, err)
		var fileBuf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&fileBuf, protocol.FrameControl, filePayload))

		// Send simultaneously
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, mouseBuf.Bytes()))
		}()
		go func() {
			defer wg.Done()
			require.NoError(t, agentConn.Write(wsCtx, websocket.MessageBinary, fileBuf.Bytes()))
		}()
		wg.Wait()

		// Agent receives MouseMove from browser
		_, agentData, err := agentConn.Read(wsCtx)
		require.NoError(t, err)
		agentFT, agentPayload, err := codec.ReadFrame(bytes.NewReader(agentData))
		require.NoError(t, err)
		assert.Equal(t, protocol.FrameControl, agentFT)
		agentMsg, err := codec.DecodeControl(agentPayload)
		require.NoError(t, err)
		assert.Equal(t, protocol.MsgMouseMove, agentMsg.Type)

		// Browser receives FileListResponse from agent
		_, browserData, err := browserConn.Read(wsCtx)
		require.NoError(t, err)
		browserFT, browserPayload, err := codec.ReadFrame(bytes.NewReader(browserData))
		require.NoError(t, err)
		assert.Equal(t, protocol.FrameControl, browserFT)
		browserMsg, err := codec.DecodeControl(browserPayload)
		require.NoError(t, err)
		assert.Equal(t, protocol.MsgFileListResponse, browserMsg.Type)
		assert.Equal(t, "/home", browserMsg.Path)
		require.Len(t, browserMsg.Entries, 1)
		assert.Equal(t, "test.txt", browserMsg.Entries[0].Name)
	})
}

func TestRelayBidirectionalConcurrent(t *testing.T) {
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
