package transport

import (
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseChannelOpen(t *testing.T) {
	co, err := ParseChannelOpen(channelOpenHeader("forwarded-tcpip"))
	require.NoError(t, err)
	assert.Equal(t, "forwarded-tcpip", co.ChannelType)
	assert.Equal(t, uint32(1), co.SenderChannel)
	assert.Equal(t, DefaultWindowSize, co.InitialWindowSz)
	assert.Equal(t, DefaultMaxPacketSize, co.MaxPacketSz)
}

func TestWriteReadChannelData(t *testing.T) {
	payload := []byte("hello AMT")
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteChannelData(w, 42, payload) })
	assert.Equal(t, APFChannelData, msgType)

	cd, err := ParseChannelData(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), cd.RecipientChannel)
	assert.Equal(t, payload, cd.Data)
}

func TestWriteReadChannelClose(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteChannelClose(w, 7) })
	assert.Equal(t, APFChannelClose, msgType)
	assert.Equal(t, uint32(7), binary.BigEndian.Uint32(raw))
}

func TestWriteReadChannelOpenConfirm(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteChannelOpenConfirm(w, 1, 2, 0x4000, 0x4000) })
	assert.Equal(t, APFChannelOpenConfirm, msgType)
	assert.Equal(t, uint32(1), binary.BigEndian.Uint32(raw[0:4]))
	assert.Equal(t, uint32(2), binary.BigEndian.Uint32(raw[4:8]))
}

func TestWriteReadChannelWindowAdj(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteChannelWindowAdj(w, 3, 8192) })
	assert.Equal(t, APFChannelWindowAdj, msgType)
	assert.Equal(t, uint32(3), binary.BigEndian.Uint32(raw[0:4]))
	assert.Equal(t, uint32(8192), binary.BigEndian.Uint32(raw[4:8]))
}

func TestParseChannelDataTooShort(t *testing.T) {
	_, err := ParseChannelData([]byte{0, 0})
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

func TestParseChannelDataBadDataString(t *testing.T) {
	_, err := ParseChannelData(joinBytes(encodeUint32(1), []byte{0, 0}))
	assert.ErrorIs(t, err, ErrMessageTooShort)
}
