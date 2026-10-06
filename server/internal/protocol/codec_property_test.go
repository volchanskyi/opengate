package protocol

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func drawBytes(t *rapid.T, label string, minLen, maxLen int) []byte {
	return rapid.SliceOfN(rapid.Byte(), minLen, maxLen).Draw(t, label)
}

func drawNonceAndHash(t *rapid.T) (nonce [32]byte, hash [48]byte) {
	copy(nonce[:], drawBytes(t, "nonce", 32, 32))
	copy(hash[:], drawBytes(t, "hash", 48, 48))
	return nonce, hash
}

func requireHandshakeType(t *rapid.T, want byte, enc []byte) {
	got, err := DecodeHandshakeType(enc)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestProperty_Frame_RoundTrip(t *testing.T) {
	t.Parallel()
	c := &Codec{}
	rapid.Check(t, func(t *rapid.T) {
		ft := rapid.SampledFrom([]byte{
			FrameControl, FrameDesktop, FrameTerminal, FrameFile,
		}).Draw(t, "frameType")
		payload := drawBytes(t, "payload", 0, 16384)

		var buf bytes.Buffer
		require.NoError(t, c.WriteFrame(&buf, ft, payload))

		gotType, gotPayload, err := c.ReadFrame(&buf)
		require.NoError(t, err)
		require.Equal(t, ft, gotType)
		require.Equal(t, payload, gotPayload)
	})
}

func TestProperty_PingPong_RoundTrip(t *testing.T) {
	t.Parallel()
	c := &Codec{}
	rapid.Check(t, func(t *rapid.T) {
		ft := rapid.SampledFrom([]byte{FramePing, FramePong}).Draw(t, "frameType")
		payload := drawBytes(t, "ignoredPayload", 0, 32)

		var buf bytes.Buffer
		require.NoError(t, c.WriteFrame(&buf, ft, payload))
		require.Equal(t, 1, buf.Len(), "ping/pong must be a single type byte")

		gotType, gotPayload, err := c.ReadFrame(&buf)
		require.NoError(t, err)
		require.Equal(t, ft, gotType)
		require.Nil(t, gotPayload)
	})
}

func TestProperty_ServerHello_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nonce, certHash := drawNonceAndHash(t)

		enc := EncodeServerHello(nonce, certHash)
		requireHandshakeType(t, byte(MsgServerHello), enc)

		gotNonce, gotHash, err := DecodeServerHello(enc)
		require.NoError(t, err)
		require.Equal(t, nonce, gotNonce)
		require.Equal(t, certHash, gotHash)
	})
}

func TestProperty_HandshakeType_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		msgType := rapid.SampledFrom([]byte{
			MsgServerHello, MsgAgentHello, MsgSkipAuth, MsgExpectHash,
		}).Draw(t, "msgType")
		payload := drawBytes(t, "payload", 0, 128)
		requireHandshakeType(t, msgType, EncodeHandshake(msgType, payload))

		nonce, hash := drawNonceAndHash(t)
		requireHandshakeType(t, byte(MsgAgentHello), EncodeAgentHello(nonce, hash))
		requireHandshakeType(t, byte(MsgSkipAuth), EncodeSkipAuth(hash))
	})
}

func TestProperty_HandshakeDecode_NeverPanic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		data := drawBytes(t, "data", 0, 256)

		_, _ = DecodeHandshakeType(data)
		_, _, _ = DecodeServerHello(data)
	})
}
