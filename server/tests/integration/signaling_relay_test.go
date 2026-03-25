package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"nhooyr.io/websocket"
)

// TestSignalingFlowThroughRelay exercises the full WebRTC signaling flow
// via the relay WebSocket path: SDP offer → answer → ICE candidates → SwitchAck.
// Uses fake SDP strings — the relay is message-agnostic and just forwards binary frames.
func TestSignalingFlowThroughRelay(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

	// Helper to encode and send a control message via WebSocket
	sendControl := func(conn *websocket.Conn, msg *protocol.ControlMessage) {
		t.Helper()
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)
		var buf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&buf, protocol.FrameControl, payload))
		require.NoError(t, conn.Write(wsCtx, websocket.MessageBinary, buf.Bytes()))
	}

	// Helper to read and decode a control message from WebSocket
	readControl := func(conn *websocket.Conn) *protocol.ControlMessage {
		t.Helper()
		_, data, err := conn.Read(wsCtx)
		require.NoError(t, err)
		ft, payload, err := codec.ReadFrame(bytes.NewReader(data))
		require.NoError(t, err)
		assert.Equal(t, protocol.FrameControl, ft)
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		return msg
	}

	// Start signaling state tracking on the server side
	sessionToken := "test-signaling-token-0123456789abcdef0123456789abcdef"
	state := env.sigTracker.StartSignaling(sessionToken)
	require.NotNil(t, state)
	assert.Equal(t, signaling.PhaseRelay, state.Phase())

	// 1. Browser sends SwitchToWebRTC offer → agent receives it
	fakeSDP := "v=0\r\no=- 123 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(browserConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeSDP,
	})

	// Transition tracker to Offered
	require.NoError(t, state.Transition(signaling.PhaseOffered))

	agentMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, agentMsg.Type)
	assert.Equal(t, fakeSDP, agentMsg.SDPOffer)

	// 2. Agent sends SwitchToWebRTC answer → browser receives it
	fakeAnswer := "v=0\r\no=- 456 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(agentConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeAnswer,
	})

	require.NoError(t, state.Transition(signaling.PhaseAnswered))

	browserMsg := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, browserMsg.Type)
	assert.Equal(t, fakeAnswer, browserMsg.SDPOffer)

	// 3. ICE candidate exchange
	require.NoError(t, state.Transition(signaling.PhaseICEGathering))

	sendControl(browserConn, &protocol.ControlMessage{
		Type:      protocol.MsgIceCandidate,
		Candidate: "candidate:1 1 udp 2113937151 192.168.1.1 12345 typ host",
		Mid:       "0",
	})

	iceMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgIceCandidate, iceMsg.Type)
	assert.Contains(t, iceMsg.Candidate, "candidate:1")
	assert.Equal(t, "0", iceMsg.Mid)

	// Agent sends ICE candidate back
	sendControl(agentConn, &protocol.ControlMessage{
		Type:      protocol.MsgIceCandidate,
		Candidate: "candidate:2 1 udp 2113937151 10.0.0.1 54321 typ host",
		Mid:       "0",
	})

	iceMsg2 := readControl(browserConn)
	assert.Equal(t, protocol.MsgIceCandidate, iceMsg2.Type)
	assert.Contains(t, iceMsg2.Candidate, "candidate:2")

	// 4. Both sides send SwitchAck
	sendControl(browserConn, &protocol.ControlMessage{Type: protocol.MsgSwitchAck})
	ackMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgSwitchAck, ackMsg.Type)

	// Record first ack
	complete := env.sigTracker.RecordAck(sessionToken)
	assert.False(t, complete, "need both sides to ack")

	sendControl(agentConn, &protocol.ControlMessage{Type: protocol.MsgSwitchAck})
	ackMsg2 := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchAck, ackMsg2.Type)

	// Record second ack — should complete
	complete = env.sigTracker.RecordAck(sessionToken)
	assert.True(t, complete, "both sides acked, should be complete")

	// Verify tracker state reached PhaseConnected
	assert.Equal(t, signaling.PhaseConnected, state.Phase())
	assert.Equal(t, int64(1), env.sigTracker.SuccessCount())
}

// TestSignalingTimeout verifies that if an offer is sent but no answer arrives,
// the tracker records PhaseFailed.
func TestSignalingTimeout(t *testing.T) {
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

	// Start signaling
	sessionToken := "test-timeout-token-0123456789abcdef0123456789abcdef01"
	state := env.sigTracker.StartSignaling(sessionToken)
	require.NotNil(t, state)

	// Browser sends offer
	offerMsg := &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: "v=0\r\nfake-offer\r\n",
	}
	payload, err := codec.EncodeControl(offerMsg)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, protocol.FrameControl, payload))
	require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, buf.Bytes()))

	require.NoError(t, state.Transition(signaling.PhaseOffered))
	assert.Equal(t, signaling.PhaseOffered, state.Phase())

	// Agent receives the offer (relay forwarded it)
	_, _, err = agentConn.Read(wsCtx)
	require.NoError(t, err)

	// Simulate timeout — no answer sent. Record failure.
	env.sigTracker.RecordFailure(sessionToken)
	assert.Equal(t, signaling.PhaseFailed, state.Phase())
	assert.Equal(t, int64(1), env.sigTracker.FailureCount())
}
