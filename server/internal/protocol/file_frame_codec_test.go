package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeDecodeFileFrame pins file payload MessagePack round-trips.
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
