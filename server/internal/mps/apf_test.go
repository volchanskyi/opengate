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
	data := encodeString(ServicePFwd)
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
	buf.Write(encodeString(ServiceAuth))

	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	assert.Equal(t, APFServiceRequest, msgType)

	sr, err := ParseServiceRequest(payload)
	require.NoError(t, err)
	assert.Equal(t, ServiceAuth, sr.ServiceName)
}

func TestParseUserAuthRequest(t *testing.T) {
	data := append(encodeString("admin"), encodeString(ServiceAuth)...)
	data = append(data, encodeString("digest")...)

	ua, err := ParseUserAuthRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "admin", ua.Username)
	assert.Equal(t, ServiceAuth, ua.ServiceName)
	assert.Equal(t, "digest", ua.MethodName)
}

func TestReadUserAuthRequest(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteByte(APFUserAuthRequest)
	buf.Write(encodeString("admin"))
	buf.Write(encodeString(ServiceAuth))
	buf.Write(encodeString("digest"))

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
	data := encodeString("tcpip-forward")
	data = append(data, 1) // want_reply = true
	data = append(data, encodeString("192.168.1.1")...)
	data = append(data, encodeUint32(16993)...)

	gr, err := ParseGlobalRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "tcpip-forward", gr.RequestName)
	assert.True(t, gr.WantReply)
	assert.NotEmpty(t, gr.Data)
}

func TestParseChannelOpen(t *testing.T) {
	data := encodeString("forwarded-tcpip")
	data = append(data, encodeUint32(1)...)    // sender channel
	data = append(data, encodeUint32(0x4000)...) // window
	data = append(data, encodeUint32(0x4000)...) // max packet

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

// --- helpers ---

func encodeString(s string) []byte {
	buf := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(buf, uint32(len(s)))
	copy(buf[4:], s)
	return buf
}

func encodeUint32(v uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	return buf
}
