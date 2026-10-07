package transport

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeThenRead(t *testing.T, write func(io.Writer) error) (uint8, []byte) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, write(&buf))
	msgType, payload, err := ReadMessage(&buf)
	require.NoError(t, err)
	return msgType, payload
}

func readWire(t *testing.T, wantType uint8, parts ...[]byte) []byte {
	t.Helper()
	msgType, payload, err := ReadMessage(bytes.NewReader(joinBytes(parts...)))
	require.NoError(t, err)
	assert.Equal(t, wantType, msgType)
	return payload
}

func readWireErr(t *testing.T, parts ...[]byte) error {
	t.Helper()
	_, _, err := ReadMessage(bytes.NewReader(joinBytes(parts...)))
	return err
}

func TestReadWriteServiceAccept(t *testing.T) {
	msgType, payload := writeThenRead(t, func(w io.Writer) error { return WriteServiceAccept(w, ServiceAuth) })
	assert.Equal(t, APFServiceAccept, msgType)

	sr, err := ParseServiceRequest(payload)
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
	payload := readWire(t, APFServiceRequest, []byte{APFServiceRequest}, encodeAPFString(ServiceAuth))

	sr, err := ParseServiceRequest(payload)
	require.NoError(t, err)
	assert.Equal(t, ServiceAuth, sr.ServiceName)
}

func TestParseUserAuthRequest(t *testing.T) {
	data := joinBytes(encodeAPFString("admin"), encodeAPFString(ServiceAuth), encodeAPFString("digest"))

	ua, err := ParseUserAuthRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "admin", ua.Username)
	assert.Equal(t, ServiceAuth, ua.ServiceName)
	assert.Equal(t, "digest", ua.MethodName)
}

func TestReadUserAuthRequest(t *testing.T) {
	payload := readWire(t, APFUserAuthRequest, []byte{APFUserAuthRequest},
		encodeAPFString("admin"), encodeAPFString(ServiceAuth), encodeAPFString("digest"))

	ua, err := ParseUserAuthRequest(payload)
	require.NoError(t, err)
	assert.Equal(t, "admin", ua.Username)
}

func TestWriteReadUserAuthSuccess(t *testing.T) {
	msgType, payload := writeThenRead(t, WriteUserAuthSuccess)
	assert.Equal(t, APFUserAuthSuccess, msgType)
	assert.Nil(t, payload)
}

func TestParseGlobalRequest(t *testing.T) {
	data := joinBytes(encodeAPFString("tcpip-forward"), []byte{1},
		encodeAPFString("192.168.1.1"), encodeUint32(16993))

	gr, err := ParseGlobalRequest(data)
	require.NoError(t, err)
	assert.Equal(t, "tcpip-forward", gr.RequestName)
	assert.True(t, gr.WantReply)
	assert.NotEmpty(t, gr.Data)
}

func TestWriteReadDisconnect(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteDisconnect(w, APFDisconnectByApp) })
	assert.Equal(t, APFDisconnect, msgType)
	assert.Equal(t, APFDisconnectByApp, binary.BigEndian.Uint32(raw))
}

func TestWriteReadProtocolVersion(t *testing.T) {
	msgType, raw := writeThenRead(t, func(w io.Writer) error { return WriteProtocolVersion(w, 1, 0, 2) })
	assert.Equal(t, APFProtocolVersion, msgType)

	pv, err := ParseProtocolVersion(raw)
	require.NoError(t, err)
	assert.Equal(t, uint32(1), pv.MajorVersion)
	assert.Equal(t, uint32(0), pv.MinorVersion)
	assert.Equal(t, uint32(2), pv.Trigger)
}

func TestWriteReadRequestSuccess(t *testing.T) {
	msgType, _ := writeThenRead(t, WriteRequestSuccess)
	assert.Equal(t, APFRequestSuccess, msgType)
}

func TestReadMessageUnknownType(t *testing.T) {
	assert.ErrorIs(t, readWireErr(t, []byte{255}), ErrUnknownMessageType)
}

func TestReadMessageEOF(t *testing.T) {
	assert.Error(t, readWireErr(t))
}

func TestParseProtocolVersionTooShort(t *testing.T) {
	_, err := ParseProtocolVersion(make([]byte, 10))
	assert.ErrorIs(t, err, ErrMessageTooShort)
}

func TestReadStringMsgOversized(t *testing.T) {
	err := readWireErr(t, []byte{APFServiceRequest},
		encodeUint32(uint32(maxAPFStringLen+1)), make([]byte, maxAPFStringLen+1))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

func TestReadUserAuthRequestOversized(t *testing.T) {
	err := readWireErr(t, []byte{APFUserAuthRequest}, encodeAPFString("admin"),
		encodeUint32(uint32(maxAPFStringLen+1)), make([]byte, maxAPFStringLen+1), encodeAPFString("digest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

func encodeUint32(v uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, v)
	return buf
}
