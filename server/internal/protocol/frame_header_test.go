package protocol

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAFramesHeaderIsReadBeforeItsPayload(t *testing.T) {
	codec := &Codec{}
	var buf bytes.Buffer
	require.NoError(t, codec.WriteFrame(&buf, FrameControl, []byte("hello")))

	frameType, length, err := codec.ReadFrameHeader(&buf)
	require.NoError(t, err)
	assert.Equal(t, FrameControl, frameType)
	assert.Equal(t, uint32(5), length)
	assert.Equal(t, 5, buf.Len(), "the payload is still unread")

	payload, err := codec.ReadPayload(&buf, length)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), payload)
}

func TestAPingHeaderCarriesNoLength(t *testing.T) {
	codec := &Codec{}
	frameType, length, err := codec.ReadFrameHeader(bytes.NewReader([]byte{FramePing}))
	require.NoError(t, err)
	assert.Equal(t, FramePing, frameType)
	assert.Zero(t, length)
}

func TestAHeaderOfAnUnknownFrameIsRefused(t *testing.T) {
	codec := &Codec{}
	_, _, err := codec.ReadFrameHeader(bytes.NewReader([]byte{0x7f, 0, 0, 0, 1}))
	require.ErrorIs(t, err, ErrUnknownFrameType)
}

func TestAPayloadCutShortIsIncomplete(t *testing.T) {
	codec := &Codec{}
	_, err := codec.ReadPayload(bytes.NewReader([]byte("he")), 5)
	require.ErrorIs(t, err, ErrIncompleteFrame)
}
