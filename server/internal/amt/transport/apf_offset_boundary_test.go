package transport

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGlobalRequest_OffsetBoundary(t *testing.T) {
	data := rawAPFString(1, []byte("x"))
	require.Len(t, data, 5)
	_, err := ParseGlobalRequest(data)
	require.ErrorIs(t, err, ErrMessageTooShort)

	gr, err := ParseGlobalRequest(joinBytes(data, []byte{1}))
	require.NoError(t, err)
	assert.Equal(t, "x", gr.RequestName)
	assert.True(t, gr.WantReply)
}

func TestParseChannelOpen_OffsetBoundary(t *testing.T) {
	header := encodeAPFString("ch")
	_, err := ParseChannelOpen(joinBytes(header, make([]byte, 11)))
	require.ErrorIs(t, err, ErrMessageTooShort)

	co, err := ParseChannelOpen(joinBytes(header, make([]byte, 12)))
	require.NoError(t, err)
	assert.Equal(t, "ch", co.ChannelType)
}

func TestParseChannelData_LenBoundary(t *testing.T) {
	cd, err := ParseChannelData(make([]byte, 8))
	require.NoError(t, err)
	assert.Equal(t, uint32(0), cd.RecipientChannel)
	assert.Empty(t, cd.Data)

	gates := []struct {
		size    int
		present bool
		msg     string
	}{
		{3, false, "3-byte input must hit the length gate, not the readString path"},
		{4, true, "4-byte input must pass the length gate and fail in readString"},
	}
	for _, g := range gates {
		_, err = ParseChannelData(make([]byte, g.size))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMessageTooShort)
		assert.Equal(t, g.present, bytes.Contains([]byte(err.Error()), []byte("data payload")), g.msg)
	}
}

func TestReadGlobalRequest_NameDispatch(t *testing.T) {
	tests := []struct {
		name      string
		wire      []byte
		wantName  string
		wantEmpty bool
	}{
		{"non-tcpip name consumes no forward data",
			joinBytes([]byte{APFGlobalRequest}, encodeAPFString("unknown"), []byte{0}), "unknown", true},
		{"tcpip-forward keeps forward data",
			joinBytes([]byte{APFGlobalRequest}, encodeAPFString("tcpip-forward"), []byte{1},
				encodeAPFString("0.0.0.0"), encodeUint32(16993)), "tcpip-forward", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mt, raw, err := ReadMessage(bytes.NewReader(tc.wire))
			require.NoError(t, err)
			assert.Equal(t, APFGlobalRequest, mt)
			gr, err := ParseGlobalRequest(raw)
			require.NoError(t, err)
			assert.Equal(t, tc.wantName, gr.RequestName)
			assert.Equal(t, tc.wantEmpty, len(gr.Data) == 0)
		})
	}
}

func TestReadChannelOpen_TypeDispatch(t *testing.T) {
	tests := []struct {
		name      string
		wire      []byte
		wantType  string
		wantEmpty bool
	}{
		{"session type consumes no extra data",
			joinBytes([]byte{APFChannelOpen}, channelOpenHeader("session")), "session", true},
		{"direct-tcpip keeps its addresses",
			joinBytes([]byte{APFChannelOpen}, channelOpenHeader("direct-tcpip"),
				encodeAPFString("0.0.0.0"), encodeUint32(80), encodeAPFString("1.2.3.4"), encodeUint32(81)),
			"direct-tcpip", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mt, raw, err := ReadMessage(bytes.NewReader(tc.wire))
			require.NoError(t, err)
			assert.Equal(t, APFChannelOpen, mt)
			co, err := ParseChannelOpen(raw)
			require.NoError(t, err)
			assert.Equal(t, tc.wantType, co.ChannelType)
			assert.Equal(t, tc.wantEmpty, len(co.Data) == 0)
		})
	}
}

func TestReadString_OffsetBoundary(t *testing.T) {
	gr, err := ParseGlobalRequest(make([]byte, 5))
	require.NoError(t, err)
	assert.Equal(t, "", gr.RequestName)

	for _, short := range [][]byte{make([]byte, 3), {0, 0, 0, 2, 'a'}} {
		_, err = ParseGlobalRequest(short)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrMessageTooShort)
	}
}
