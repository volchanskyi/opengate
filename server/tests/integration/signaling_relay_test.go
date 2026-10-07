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

func TestSignalingFlowThroughRelay(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

	sendControl := func(conn *websocket.Conn, msg *protocol.ControlMessage) {
		t.Helper()
		payload, err := codec.EncodeControl(msg)
		require.NoError(t, err)
		var buf bytes.Buffer
		require.NoError(t, codec.WriteFrame(&buf, protocol.FrameControl, payload))
		require.NoError(t, conn.Write(wsCtx, websocket.MessageBinary, buf.Bytes()))
	}

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

	fakeSDP := "v=0\r\no=- 123 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(browserConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeSDP,
	})

	agentMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, agentMsg.Type)
	assert.Equal(t, fakeSDP, agentMsg.SDPOffer)

	fakeAnswer := "v=0\r\no=- 456 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\n"
	sendControl(agentConn, &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: fakeAnswer,
	})

	browserMsg := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchToWebRTC, browserMsg.Type)
	assert.Equal(t, fakeAnswer, browserMsg.SDPOffer)

	sendControl(browserConn, &protocol.ControlMessage{
		Type:      protocol.MsgIceCandidate,
		Candidate: "candidate:1 1 udp 2113937151 192.168.1.1 12345 typ host",
		Mid:       "0",
	})

	iceMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgIceCandidate, iceMsg.Type)
	assert.Contains(t, iceMsg.Candidate, "candidate:1")
	assert.Equal(t, "0", iceMsg.Mid)

	sendControl(agentConn, &protocol.ControlMessage{
		Type:      protocol.MsgIceCandidate,
		Candidate: "candidate:2 1 udp 2113937151 10.0.0.1 54321 typ host",
		Mid:       "0",
	})

	iceMsg2 := readControl(browserConn)
	assert.Equal(t, protocol.MsgIceCandidate, iceMsg2.Type)
	assert.Contains(t, iceMsg2.Candidate, "candidate:2")

	sendControl(browserConn, &protocol.ControlMessage{Type: protocol.MsgSwitchAck})
	ackMsg := readControl(agentConn)
	assert.Equal(t, protocol.MsgSwitchAck, ackMsg.Type)

	sendControl(agentConn, &protocol.ControlMessage{Type: protocol.MsgSwitchAck})
	ackMsg2 := readControl(browserConn)
	assert.Equal(t, protocol.MsgSwitchAck, ackMsg2.Type)
}

func TestSignalingOfferReachesTheAgentWithNoAnswer(t *testing.T) {
	t.Parallel()
	env := newSessionTestEnv(t)
	ctx := context.Background()

	agentConn, browserConn := env.setupRelayPair(t, ctx)
	wsCtx, wsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer wsCancel()

	codec := &protocol.Codec{}

	offerMsg := &protocol.ControlMessage{
		Type:     protocol.MsgSwitchToWebRTC,
		SDPOffer: "v=0\r\nfake-offer\r\n",
	}
	payload, err := codec.EncodeControl(offerMsg)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, protocol.FrameControl, payload))
	require.NoError(t, browserConn.Write(wsCtx, websocket.MessageBinary, buf.Bytes()))

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
