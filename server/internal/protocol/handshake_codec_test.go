package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeHandshake pins raw binary handshake prefix encoding.
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
		for _, msgType := range []byte{MsgServerHello, MsgAgentHello, MsgSkipAuth, MsgExpectHash} {
			result := EncodeHandshake(msgType, []byte{0x01})
			assert.Equal(t, msgType, result[0])
		}
	})
}

// TestDecodeHandshakeType pins active and retired handshake type handling.
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

	t.Run("retired proof types", func(t *testing.T) {
		for _, data := range [][]byte{{0x12}, {0x13}} {
			got, err := DecodeHandshakeType(data)
			require.Error(t, err)
			assert.Zero(t, got)
		}
	})
}
