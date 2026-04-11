package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDecodeTerminalFrame(t *testing.T) {
	codec := &Codec{}

	t.Run("round trip", func(t *testing.T) {
		orig := &TerminalFrame{Data: []byte("hello terminal")}
		data, err := codec.EncodeTerminalFrame(orig)
		require.NoError(t, err)

		decoded, err := codec.DecodeTerminalFrame(data)
		require.NoError(t, err)
		assert.Equal(t, orig.Data, decoded.Data)
	})

	t.Run("empty data", func(t *testing.T) {
		orig := &TerminalFrame{Data: []byte{}}
		data, err := codec.EncodeTerminalFrame(orig)
		require.NoError(t, err)

		decoded, err := codec.DecodeTerminalFrame(data)
		require.NoError(t, err)
		assert.Empty(t, decoded.Data)
	})

	t.Run("invalid data", func(t *testing.T) {
		_, err := codec.DecodeTerminalFrame([]byte{0xFF, 0xFE})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode terminal frame")
	})
}

func TestEncodeDecodeFileFrame(t *testing.T) {
	codec := &Codec{}

	t.Run("round trip", func(t *testing.T) {
		orig := &FileFrame{Offset: 1024, TotalSize: 4096, Data: []byte("chunk")}
		data, err := codec.EncodeFileFrame(orig)
		require.NoError(t, err)

		decoded, err := codec.DecodeFileFrame(data)
		require.NoError(t, err)
		assert.Equal(t, orig.Offset, decoded.Offset)
		assert.Equal(t, orig.TotalSize, decoded.TotalSize)
		assert.Equal(t, orig.Data, decoded.Data)
	})

	t.Run("zero offset", func(t *testing.T) {
		orig := &FileFrame{Offset: 0, TotalSize: 100, Data: []byte("start")}
		data, err := codec.EncodeFileFrame(orig)
		require.NoError(t, err)

		decoded, err := codec.DecodeFileFrame(data)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), decoded.Offset)
	})

	t.Run("invalid data", func(t *testing.T) {
		_, err := codec.DecodeFileFrame([]byte{0xFF, 0xFE})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode file frame")
	})
}

func TestEncodeHandshake(t *testing.T) {
	t.Run("with payload", func(t *testing.T) {
		payload := []byte("handshake-payload")
		result := EncodeHandshake(MsgServerHello, payload)
		assert.Equal(t, MsgServerHello, result[0])
		assert.Equal(t, payload, result[1:])
	})

	t.Run("empty payload", func(t *testing.T) {
		result := EncodeHandshake(MsgSkipAuth, nil)
		assert.Len(t, result, 1)
		assert.Equal(t, MsgSkipAuth, result[0])
	})

	t.Run("all message types", func(t *testing.T) {
		for _, msgType := range []byte{MsgServerHello, MsgAgentHello, MsgServerProof, MsgAgentProof, MsgSkipAuth, MsgExpectHash} {
			result := EncodeHandshake(msgType, []byte{0x01})
			assert.Equal(t, msgType, result[0])
		}
	})
}

func TestDecodeHandshakeType(t *testing.T) {
	t.Run("valid types", func(t *testing.T) {
		tests := []struct {
			name string
			data []byte
			want byte
		}{
			{"server hello", []byte{MsgServerHello, 0x01}, MsgServerHello},
			{"agent hello", []byte{MsgAgentHello}, MsgAgentHello},
			{"skip auth", []byte{MsgSkipAuth, 0xFF, 0xFE}, MsgSkipAuth},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := DecodeHandshakeType(tt.data)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("empty data", func(t *testing.T) {
		_, err := DecodeHandshakeType([]byte{})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty handshake data")
	})
}
