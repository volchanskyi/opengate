package transport

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func rawAPFString(strLen uint32, body []byte) []byte {
	out := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(out[:4], strLen)
	copy(out[4:], body)
	return out
}

func joinBytes(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}

func channelOpenHeader(chType string) []byte {
	return joinBytes(encodeAPFString(chType), encodeUint32(1), encodeUint32(0x8000), encodeUint32(0x8000))
}

func TestAPFRead_StringLenBoundaries(t *testing.T) {
	const max = uint32(maxAPFStringLen)
	const tooLong = max + 1

	exactBody := bytes.Repeat([]byte("a"), int(max))
	maxStr := rawAPFString(max, exactBody)
	overStr := rawAPFString(tooLong, nil)

	cases := []struct {
		name      string
		msgType   uint8
		ok        []byte
		bad       []byte
		errSubstr string
	}{
		{
			name:      "service_request",
			msgType:   APFServiceRequest,
			ok:        maxStr,
			bad:       overStr,
			errSubstr: "service name too long",
		},
		{
			name:      "user_auth_request",
			msgType:   APFUserAuthRequest,
			ok:        joinBytes(maxStr, encodeAPFString(ServiceAuth), encodeAPFString("digest")),
			bad:       overStr,
			errSubstr: "auth string too long",
		},
		{
			name:      "global_request_name_len",
			msgType:   APFGlobalRequest,
			ok:        joinBytes(maxStr, []byte{0}),
			bad:       overStr,
			errSubstr: "request name too long",
		},
		{
			name:      "global_request_forward_addr_len",
			msgType:   APFGlobalRequest,
			ok:        joinBytes(encodeAPFString("tcpip-forward"), []byte{1}, maxStr, encodeUint32(16993)),
			bad:       joinBytes(encodeAPFString("tcpip-forward"), []byte{1}, overStr),
			errSubstr: "forward address too long",
		},
		{
			name:      "channel_open_type_len",
			msgType:   APFChannelOpen,
			ok:        joinBytes(maxStr, encodeUint32(1), encodeUint32(0x8000), encodeUint32(0x8000)),
			bad:       overStr,
			errSubstr: "channel type too long",
		},
		{
			name:    "channel_open_extra_addr_len",
			msgType: APFChannelOpen,
			ok: joinBytes(channelOpenHeader("direct-tcpip"),
				maxStr, encodeUint32(80), maxStr, encodeUint32(80)),
			bad:       joinBytes(channelOpenHeader("direct-tcpip"), overStr),
			errSubstr: "address too long",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/exact_max_ok", func(t *testing.T) {
			gotType, _, err := ReadMessage(bytes.NewReader(append([]byte{tc.msgType}, tc.ok...)))
			require.NoError(t, err, "max-length string must be accepted, not error")
			assert.Equal(t, tc.msgType, gotType)
		})
		t.Run(tc.name+"/over_max_errors", func(t *testing.T) {
			_, _, err := ReadMessage(bytes.NewReader(append([]byte{tc.msgType}, tc.bad...)))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSubstr)
		})
	}
}

func TestWriteChannelData_PayloadCap(t *testing.T) {
	exact := make([]byte, maxAPFPayload)
	var buf bytes.Buffer
	require.NoError(t, WriteChannelData(&buf, 1, exact))

	tooBig := make([]byte, maxAPFPayload+1)
	err := WriteChannelData(io.Discard, 1, tooBig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel data too large")
}

func TestReadChannelData_LenCap(t *testing.T) {
	const limit = uint32(1 << 20)
	wire := joinBytes([]byte{APFChannelData}, encodeUint32(1), encodeUint32(limit), make([]byte, limit))
	mt, raw, err := ReadMessage(bytes.NewReader(wire))
	require.NoError(t, err)
	assert.Equal(t, APFChannelData, mt)
	assert.Equal(t, 8+int(limit), len(raw))

	wireBad := joinBytes([]byte{APFChannelData}, encodeUint32(1), encodeUint32(limit+1))
	_, _, err = ReadMessage(bytes.NewReader(wireBad))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channel data too large")
}

func TestWriteStringMsg_LenCap(t *testing.T) {
	exact := string(bytes.Repeat([]byte("a"), maxAPFStringLen))
	var buf bytes.Buffer
	require.NoError(t, WriteServiceAccept(&buf, exact))

	tooBig := exact + "a"
	err := WriteServiceAccept(io.Discard, tooBig)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "string too long")
}
