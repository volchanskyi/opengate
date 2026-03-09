package mps

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadWriteServiceAccept(t *testing.T) {
	var buf bytes.Buffer
	err := WriteServiceAccept(&buf, ServiceAuth)
	require.NoError(t, err)

	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFServiceAccept, msgType)

	sr, err := ParseServiceRequest(payload) // same format
	require.NoError(t, err)
	assert.Equal(t, ServiceAuth, sr.ServiceName)
}

func TestParseServiceRequest(t *testing.T) {
	data := encodeAPFString(ServicePFwd)
	sr, err := ParseServiceRequest(data)
	require.NoError(t, err)
	assert.Equal(t, ServicePFwd, sr.ServiceName)

	t.Run("too short", func(t *testing.T) {
		_, err := ParseServiceRequest([]byte{0, 0})
		assert.Error(t, err)
	})
}

func TestReadServiceRequest(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(APFServiceRequest)
	buf.Write(encodeAPFString(ServiceAuth))

	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFServiceRequest, msgType)

	sr, err := ParseServiceRequest(payload)
	require.NoError(t, err)
	assert.Equal(t, ServiceAuth, sr.ServiceName)
}

func TestParseUserAuthRequest(t *testing.T) {
	data := append(encodeAPFString("admin"), encodeAPFString(ServiceAuth)...)
	data = append(data, encodeAPFString("digest")...)

	ua, err := ParseUserAuthRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "admin", ua.Username)
	assert.Equal(t, ServiceAuth, ua.ServiceName)
	assert.Equal(t, "digest", ua.MethodName)
}

func TestReadUserAuthRequest(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(APFUserAuthRequest)
	buf.Write(encodeAPFString("admin"))
	buf.Write(encodeAPFString(ServiceAuth))
	buf.Write(encodeAPFString("digest"))

	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFUserAuthRequest, msgType)

	ua, err := ParseUserAuthRequest(payload)
	require.NoError(t, err)
	assert.Equal(t, "admin", ua.Username)
}

func TestWriteReadUserAuthSuccess(t *testing.T) {
	var buf bytes.Buffer
	err := WriteUserAuthSuccess(&buf)
	require.NoError(t, err)

	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFUserAuthSuccess, msgType)
	assert.Nil(t, payload)
}

func TestParseGlobalRequest(t *testing.T) {
	data := encodeAPFString("tcpip-forward")
	data = append(data, 1) // want_reply = true
	data = append(data, encodeAPFString("192.168.1.1")...)
	data = append(data, encodeUint32(16993)...)

	gr, err := ParseGlobalRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "tcpip-forward", gr.RequestName)
	assert.True(t, gr.WantReply)
	assert.NotEmpty(t, gr.Data)
}

func TestParseChannelOpen(t *testing.T) {
	data := encodeAPFString("forwarded-tcpip")
	data = append(data, encodeUint32(1)...)         // sender channel
	data = append(data, encodeUint32(0x8000)...)     // window
	data = append(data, encodeUint32(0x8000)...)     // max packet

	co, err := ParseChannelOpen(data)
	require.NoError(t, err)
	assert.Equal(t, "forwarded-tcpip", co.ChannelType)
	assert.Equal(t, uint32(1), co.SenderChannel)
	assert.Equal(t, DefaultWindowSize, co.InitialWindowSz)
	assert.Equal(t, DefaultMaxPacketSize, co.MaxPacketSz)
}

func TestWriteReadChannelData(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("hello AMT")
	err := WriteChannelData(&buf, 42, payload)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFChannelData, msgType)

	cd, err := ParseChannelData(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(42), cd.RecipientChannel)
	assert.Equal(t, payload, cd.Data)
}

func TestWriteReadChannelClose(t *testing.T) {
	var buf bytes.Buffer
	err := WriteChannelClose(&buf, 7)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFChannelClose, msgType)
	assert.Equal(t, uint32(7), binary.BigEndian.Uint32(raw))
}

func TestWriteReadChannelOpenConfirm(t *testing.T) {
	var buf bytes.Buffer
	err := WriteChannelOpenConfirm(&buf, 1, 2, 0x4000, 0x4000)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFChannelOpenConfirm, msgType)
	assert.Equal(t, uint32(1), binary.BigEndian.Uint32(raw[0:4]))
	assert.Equal(t, uint32(2), binary.BigEndian.Uint32(raw[4:8]))
}

func TestWriteReadChannelWindowAdj(t *testing.T) {
	var buf bytes.Buffer
	err := WriteChannelWindowAdj(&buf, 3, 8192)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFChannelWindowAdj, msgType)
	assert.Equal(t, uint32(3), binary.BigEndian.Uint32(raw[0:4]))
	assert.Equal(t, uint32(8192), binary.BigEndian.Uint32(raw[4:8]))
}

func TestWriteReadDisconnect(t *testing.T) {
	var buf bytes.Buffer
	err := WriteDisconnect(&buf, APFDisconnectByApp)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFDisconnect, msgType)
	assert.Equal(t, APFDisconnectByApp, binary.BigEndian.Uint32(raw))
}

func TestWriteReadProtocolVersion(t *testing.T) {
	var buf bytes.Buffer
	err := WriteProtocolVersion(&buf, 1, 0, 2)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFProtocolVersion, msgType)

	pv, err := ParseProtocolVersion(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), pv.MajorVersion)
	assert.Equal(t, uint32(0), pv.MinorVersion)
	assert.Equal(t, uint32(2), pv.Trigger)
}

func TestWriteReadRequestSuccess(t *testing.T) {
	var buf bytes.Buffer
	err := WriteRequestSuccess(&buf)
	require.NoError(t, err)

	msgType, _, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFRequestSuccess, msgType)
}

func TestReadMessageUnknownType(t *testing.T) {
	buf := bytes.NewBuffer([]byte{255})
	_, _, err := ReadMessage(buf)
	assert.ErrorIs(t, err, ErrUnknownMessageType)
}

func TestReadMessageEOF(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	_, _, err := ReadMessage(buf)
	assert.Error(t, err)
}

func TestParseChannelDataTooShort(t *testing.T) {
	_, err := ParseChannelData([]byte{0, 0})
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

func TestParseProtocolVersionTooShort(t *testing.T) {
	_, err := ParseProtocolVersion(make([]byte, 10))
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

// --- Stage 1: Keepalive tests ---

func TestWriteReadKeepaliveRequestRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	err := WriteKeepaliveRequest(&buf, 0xDEADBEEF)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFKeepaliveRequest, msgType)

	ka, err := ParseKeepaliveRequest(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(0xDEADBEEF), ka.Cookie)
}

func TestWriteReadKeepaliveReplyRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	err := WriteKeepaliveReply(&buf, 0xCAFEBABE)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFKeepaliveReply, msgType)

	ka, err := ParseKeepaliveRequest(raw) // same wire format as request
	require.NoError(t, err)
	assert.Equal(t, uint32(0xCAFEBABE), ka.Cookie)
}

func TestWriteReadKeepaliveOptionsRoundtrip(t *testing.T) {
	var buf bytes.Buffer
	err := WriteKeepaliveOptionsRequest(&buf, 30, 10)
	require.NoError(t, err)

	msgType, raw, err := ReadMessage(&buf)
	require.NoError(t, err)
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
		{"keepalive_options_request", APFKeepaliveOptionsRequest, append(encodeUint32(30), encodeUint32(10)...), 8},
		{"keepalive_options_reply", APFKeepaliveOptionsReply, append(encodeUint32(60), encodeUint32(20)...), 8},
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

// --- Stage 2: GUID reorder + ParseForwardData tests ---

func TestReorderIntelGUID(t *testing.T) {
	// Intel AMT GUID raw bytes from ProtocolVersion message.
	// Raw LE-encoded GUID: 01020304-0506-0708-090A-0B0C0D0E0F10
	// Intel byte order: first 3 groups are LE, last 2 are BE.
	raw := [16]byte{
		0x04, 0x03, 0x02, 0x01, // group 1 LE → 01020304
		0x06, 0x05, // group 2 LE → 0506
		0x08, 0x07, // group 3 LE → 0708
		0x09, 0x0A, // group 4 BE → 090A
		0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, // group 5 BE
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
			data:     append(encodeAPFString("192.168.1.1"), encodeUint32(16992)...),
			wantAddr: "192.168.1.1",
			wantPort: 16992,
		},
		{
			name:     "empty address",
			data:     append(encodeAPFString(""), encodeUint32(16993)...),
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

// --- error path tests ---

func TestReadStringMsgOversized(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(APFServiceRequest)
	// Write a string length exceeding maxAPFStringLen.
	binary.Write(&buf, binary.BigEndian, uint32(maxAPFStringLen+1)) //nolint:errcheck
	buf.Write(make([]byte, maxAPFStringLen+1))

	_, _, err := ReadMessage(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

func TestReadUserAuthRequestOversized(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(APFUserAuthRequest)
	// First string is valid, second is oversized.
	buf.Write(encodeAPFString("admin"))
	binary.Write(&buf, binary.BigEndian, uint32(maxAPFStringLen+1)) //nolint:errcheck
	buf.Write(make([]byte, maxAPFStringLen+1))
	buf.Write(encodeAPFString("digest"))

	_, _, err := ReadMessage(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

func TestParseChannelDataBadDataString(t *testing.T) {
	// Valid channel ID (4 bytes) but truncated data string length.
	data := encodeUint32(1)
	data = append(data, 0, 0) // only 2 bytes, need 4 for string length
	_, err := ParseChannelData(data)
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

// --- helpers ---

func encodeUint32(v uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	return buf
}
