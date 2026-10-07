package transport

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteReadKeepaliveRequestRoundtrip(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteKeepaliveRequest(w, 0xDEADBEEF) })
	assert.Equal(t, APFKeepaliveRequest, msgType)

	ka, err := ParseKeepaliveRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(0xDEADBEEF), ka.Cookie)
}

func TestWriteReadKeepaliveReplyRoundtrip(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteKeepaliveReply(w, 0xCAFEBABE) })
	assert.Equal(t, APFKeepaliveReply, msgType)

	ka, err := ParseKeepaliveRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(0xCAFEBABE), ka.Cookie)
}

func TestWriteReadKeepaliveOptionsRoundtrip(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteKeepaliveOptionsRequest(w, 30, 10) })
	assert.Equal(t, APFKeepaliveOptionsRequest, msgType)

	ko, err := ParseKeepaliveOptions(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(30), ko.Interval)
	assert.Equal(t, uint32(10), ko.Timeout)
}

func TestReadMessageKeepaliveTypes(t *testing.T) {
	tests := []struct {
		name    string
		msgType uint8
		payload []byte
		wantLen int
	}{
		{"keepalive_request", APFKeepaliveRequest, encodeUint32(1), 4},
		{"keepalive_reply", APFKeepaliveReply, encodeUint32(2), 4},
		{"keepalive_options_request", APFKeepaliveOptionsRequest, joinBytes(encodeUint32(30), encodeUint32(10)), 8},
		{"keepalive_options_reply", APFKeepaliveOptionsReply, joinBytes(encodeUint32(60), encodeUint32(20)), 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			buf.WriteByte(tt.msgType)
			buf.Write(tt.payload)

			msgType, raw, err := ReadMessage(&buf)
			require.NoError(t, err)
			assert.Equal(t, tt.msgType, msgType)
			assert.Len(t, raw, tt.wantLen)
		})
	}
}

func TestParseKeepaliveRequestTooShort(t *testing.T) {
	_, err := ParseKeepaliveRequest([]byte{0, 0})
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

func TestParseKeepaliveOptionsTooShort(t *testing.T) {
	_, err := ParseKeepaliveOptions([]byte{0, 0, 0, 0})
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

func TestReorderIntelGUID(t *testing.T) {
	raw := [16]byte{
		0x04, 0x03, 0x02, 0x01, // group 1 little-endian
		0x06, 0x05, // group 2 little-endian
		0x08, 0x07, // group 3 little-endian
		0x09, 0x0A, // group 4 big-endian
		0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, // group 5 big-endian
	}
	result := ReorderIntelGUID(raw)
	assert.Equal(t, "01020304-0506-0708-090a-0b0c0d0e0f10", result.String())
}

func TestReorderIntelGUIDAllZeros(t *testing.T) {
	var raw [16]byte
	result := ReorderIntelGUID(raw)
	assert.Equal(t, "00000000-0000-0000-0000-000000000000", result.String())
}

func TestParseForwardData(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantAddr string
		wantPort uint32
		wantErr  bool
	}{
		{
			name:     "valid",
			data:     joinBytes(encodeAPFString("192.168.1.1"), encodeUint32(16992)),
			wantAddr: "192.168.1.1",
			wantPort: 16992,
		},
		{
			name:     "empty address",
			data:     joinBytes(encodeAPFString(""), encodeUint32(16993)),
			wantAddr: "",
			wantPort: 16993,
		},
		{
			name:    "too short for address length",
			data:    []byte{0, 0},
			wantErr: true,
		},
		{
			name:    "too short for port",
			data:    encodeAPFString("addr"),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, port, err := ParseForwardData(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantAddr, addr)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}
