package protocol

import (
	"bytes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGoldenDesktopFrame(t *testing.T) {
	data := readGolden(t, "desktop_frame.bin")
	codec := &Codec{}

	reader := bytes.NewReader(data)
	frameType, payload, err := codec.ReadFrame(reader)
	require.NoError(t, err)
	assert.Equal(t, FrameDesktop, frameType)

	frame, err := codec.DecodeDesktopFrame(payload)
	require.NoError(t, err)
	assert.Equal(t, uint64(42), frame.Sequence)
	assert.Equal(t, uint16(10), frame.X)
	assert.Equal(t, uint16(20), frame.Y)
	assert.Equal(t, uint16(1920), frame.Width)
	assert.Equal(t, uint16(1080), frame.Height)
	assert.Equal(t, EncodingZstd, frame.Encoding)
	assert.Equal(t, []byte{0xDE, 0xAD, 0xBE, 0xEF}, frame.Data)
}
