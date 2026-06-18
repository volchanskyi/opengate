package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeDecodeTerminalFrame pins terminal payload MessagePack round-trips.
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
