package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"nhooyr.io/websocket"
)

// TestSignalingFlowThroughRelay exercises the full WebRTC negotiation as it
// actually happens: SDP offer → answer → ICE candidates → SwitchAck, every one
// of them carried between the two sides by the relay.
//
// The outcome is that the relay forwards them and does not read them. It copies
// bytes between two connections without decoding a frame, which is why the
// strings here are fake and why the server has no view of the negotiation's
// progress — the two peers do, and they are the only ones who need it.
func TestSignalingFlowThroughRelay(t *testing.T) {
	t.Parallel()
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

	// 1. Browser sends SwitchToWebRTC offer → agent receives it
	fakeSDP := "v=0\r\no=- 123 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(browserConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeSDP,
	})

	agentMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, agentMsg.Type)
	assert.Equal(t, fakeSDP, agentMsg.SDPOffer)

	// 2. Agent sends SwitchToWebRTC answer → browser receives it
	fakeAnswer := "v=0\r\no=- 456 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(agentConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeAnswer,
	})

	browserMsg := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, browserMsg.Type)
	assert.Equal(t, fakeAnswer, browserMsg.SDPOffer)

	// 3. ICE candidate exchange
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

	sendControl(agentConn, &protocol.ControlMessage{Type: protocol.MsgSwitchAck})
	ackMsg2 := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchAck, ackMsg2.Type)
}

// TestSignalingOfferReachesTheAgentWithNoAnswer is the other half: an offer the
// far side never answers still crosses the relay, and the relay neither waits
// for the answer nor notices that none came. A negotiation that stalls is the
// two peers' problem, and the session stays a relayed one.
func TestSignalingOfferReachesTheAgentWithNoAnswer(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

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

	// Agent receives the offer (relay forwarded it)
	_, data, err := agentConn.Read(wsCtx)
	require.NoError(t, err)

	ft, forwarded, err := codec.ReadFrame(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, protocol.FrameControl, ft)
	arrived, err := codec.DecodeControl(forwarded)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, arrived.Type)
	assert.Equal(t, offerMsg.SDPOffer, arrived.SDPOffer,
		"the relay forwards the offer byte for byte; it does not read it")
}
