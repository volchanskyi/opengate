package protocol

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrameTypeByteValues(t *testing.T) {
	// Must match Rust constants exactly
	assert.Equal(t, byte(0x01), FrameControl)
	assert.Equal(t, byte(0x02), FrameDesktop)
	assert.Equal(t, byte(0x03), FrameTerminal)
	assert.Equal(t, byte(0x04), FrameFile)
	assert.Equal(t, byte(0x05), FramePing)
	assert.Equal(t, byte(0x06), FramePong)
}

func TestHandshakeMessageTypeValues(t *testing.T) {
	assert.Equal(t, byte(0x10), MsgServerHello)
	assert.Equal(t, byte(0x11), MsgAgentHello)
	assert.Equal(t, byte(0x12), MsgServerProof)
	assert.Equal(t, byte(0x13), MsgAgentProof)
	assert.Equal(t, byte(0x14), MsgSkipAuth)
	assert.Equal(t, byte(0x15), MsgExpectHash)
}

func TestSessionTokenGeneration(t *testing.T) {
	token := GenerateSessionToken()
	assert.Len(t, string(token), 64)
	for _, c := range string(token) {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"expected hex char, got %c", c)
	}
}

func TestSessionTokenUniqueness(t *testing.T) {
	t1 := GenerateSessionToken()
	t2 := GenerateSessionToken()
	assert.NotEqual(t, t1, t2)
}

func TestControlMessageRoundtrip(t *testing.T) {
	codec := &Codec{}

	tests := []struct {
		name string
		msg  ControlMessage
	}{
		{
			name: "AgentRegister",
			msg: ControlMessage{
				Type:         MsgAgentRegister,
				Capabilities: []AgentCapability{CapRemoteDesktop, CapTerminal},
				Hostname:     "test-machine",
				OS:           "linux",
			},
		},
		{
			name: "AgentHeartbeat",
			msg: ControlMessage{
				Type:      MsgAgentHeartbeat,
				Timestamp: 1700000000,
			},
		},
		{
			name: "RelayReady",
			msg: ControlMessage{
				Type: MsgRelayReady,
			},
		},
		{
			name: "SwitchAck",
			msg: ControlMessage{
				Type: MsgSwitchAck,
			},
		},
		{
			name: "SessionRequest",
			msg: ControlMessage{
				Type:     MsgSessionRequest,
				Token:    GenerateSessionToken(),
				RelayURL: "wss://relay.example.com/abc",
				Permissions: &Permissions{
					Desktop:   true,
					Terminal:  true,
					FileRead:  true,
					FileWrite: false,
					Input:     true,
				},
			},
		},
		{
			name: "IceCandidate",
			msg: ControlMessage{
				Type:      MsgIceCandidate,
				Candidate: "candidate:1 1 UDP ...",
				Mid:       "0",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := codec.EncodeControl(&tt.msg)
			require.NoError(t, err)

			decoded, err := codec.DecodeControl(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.msg.Type, decoded.Type)
		})
	}
}

func TestReadWriteFrameLengthPrefix(t *testing.T) {
	codec := &Codec{}

	tests := []struct {
		name      string
		frameType byte
		payload   []byte
	}{
		{"zero-byte payload", FrameControl, []byte{}},
		{"one-byte payload", FrameControl, []byte{0x42}},
		{"64KB payload", FrameDesktop, make([]byte, 65535)},
		{"1MB payload", FrameFile, make([]byte, 1024*1024)},
		{"ping (no payload)", FramePing, nil},
		{"pong (no payload)", FramePong, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := codec.WriteFrame(&buf, tt.frameType, tt.payload)
			require.NoError(t, err)

			ft, payload, err := codec.ReadFrame(&buf)
			require.NoError(t, err)
			assert.Equal(t, tt.frameType, ft)

			if tt.frameType == FramePing || tt.frameType == FramePong {
				assert.Nil(t, payload)
			} else {
				assert.Equal(t, tt.payload, payload)
			}
		})
	}
}

func TestHandshakeMessageBinaryLayout(t *testing.T) {
	var nonce [32]byte
	var certHash [48]byte
	for i := range nonce {
		nonce[i] = 0xAA
	}
	for i := range certHash {
		certHash[i] = 0xBB
	}

	// ServerHello: [0x10][32 nonce bytes][48 cert_hash bytes] = 81 bytes total
	encoded := EncodeServerHello(nonce, certHash)
	assert.Len(t, encoded, 81)
	assert.Equal(t, byte(0x10), encoded[0])
	assert.Equal(t, nonce[:], encoded[1:33])
	assert.Equal(t, certHash[:], encoded[33:81])

	// AgentHello: same structure, different type byte
	encoded = EncodeAgentHello(nonce, certHash)
	assert.Len(t, encoded, 81)
	assert.Equal(t, byte(0x11), encoded[0])

	// Decode roundtrip
	decodedNonce, decodedHash, err := DecodeServerHello(EncodeServerHello(nonce, certHash))
	require.NoError(t, err)
	assert.Equal(t, nonce, decodedNonce)
	assert.Equal(t, certHash, decodedHash)
}

func TestDesktopFrameRoundtrip(t *testing.T) {
	codec := &Codec{}
	original := &DesktopFrame{
		Sequence: 42,
		X:        10,
		Y:        20,
		Width:    1920,
		Height:   1080,
		Encoding: EncodingZstd,
		Data:     []byte{0xDE, 0xAD, 0xBE, 0xEF},
	}

	encoded, err := codec.EncodeDesktopFrame(original)
	require.NoError(t, err)

	decoded, err := codec.DecodeDesktopFrame(encoded)
	require.NoError(t, err)
	assert.Equal(t, original.Sequence, decoded.Sequence)
	assert.Equal(t, original.X, decoded.X)
	assert.Equal(t, original.Y, decoded.Y)
	assert.Equal(t, original.Width, decoded.Width)
	assert.Equal(t, original.Height, decoded.Height)
	assert.Equal(t, original.Encoding, decoded.Encoding)
	assert.Equal(t, original.Data, decoded.Data)
}

func TestReadFrameUnknownType(t *testing.T) {
	codec := &Codec{}
	data := []byte{0xFF, 0x00, 0x00, 0x00, 0x01, 0x00}
	_, _, err := codec.ReadFrame(bytes.NewReader(data))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrUnknownFrameType)
}

func TestReadFrameOversized(t *testing.T) {
	codec := &Codec{}
	// Craft a frame header claiming a payload larger than MaxFrameSize
	var buf bytes.Buffer
	_ = codec.WriteFrame(&buf, FrameControl, []byte{}) // valid empty frame
	// Manually craft an oversized frame header
	oversized := []byte{FrameControl, 0x7F, 0xFF, 0xFF, 0xFF} // ~2GB
	_, _, err := codec.ReadFrame(bytes.NewReader(oversized))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrFrameTooLarge)
}

func TestPingPongSingleByte(t *testing.T) {
	codec := &Codec{}

	// Ping
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, FramePing, nil))
	assert.Equal(t, []byte{FramePing}, buf.Bytes())

	ft, payload, err := codec.ReadFrame(&buf)
	require.NoError(t, err)
	assert.Equal(t, FramePing, ft)
	assert.Nil(t, payload)

	// Pong
	buf.Reset()
	require.NoError(t, codec.WriteFrame(&buf, FramePong, nil))
	assert.Equal(t, []byte{FramePong}, buf.Bytes())
}
