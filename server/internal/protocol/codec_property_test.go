package protocol

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// Property-based coverage for the wire envelope and handshake codecs. The
// existing FuzzReadFrame target covers decode robustness; these properties add
// the complementary *round-trip* guarantee (encode then decode recovers the
// input) plus handshake encode/decode and decode-robustness. rapid.Check always
// runs under `go test` and explores a bounded number of cases deterministically,
// per tests-determinism.md.

// TestProperty_Frame_RoundTrip asserts a length-prefixed frame survives
// WriteFrame → ReadFrame with type and payload intact, for every payload-bearing
// frame type and arbitrary payloads bounded under MaxFrameSize.
func TestProperty_Frame_RoundTrip(t *testing.T) {
	t.Parallel()
	c := &Codec{}
	rapid.Check(t, func(t *rapid.T) {
		ft := rapid.SampledFrom([]byte{
			FrameControl, FrameDesktop, FrameTerminal, FrameFile,
		}).Draw(t, "frameType")
		payload := rapid.SliceOfN(rapid.Byte(), 0, 16384).Draw(t, "payload")

		var buf bytes.Buffer
		require.NoError(t, c.WriteFrame(&buf, ft, payload))

		gotType, gotPayload, err := c.ReadFrame(&buf)
		require.NoError(t, err)
		require.Equal(t, ft, gotType)
		require.Equal(t, payload, gotPayload)
	})
}

// TestProperty_PingPong_RoundTrip asserts ping/pong are written as a bare type
// byte (no length/payload) and read back as that type with a nil payload — any
// supplied payload is intentionally dropped.
func TestProperty_PingPong_RoundTrip(t *testing.T) {
	t.Parallel()
	c := &Codec{}
	rapid.Check(t, func(t *rapid.T) {
		ft := rapid.SampledFrom([]byte{FramePing, FramePong}).Draw(t, "frameType")
		payload := rapid.SliceOfN(rapid.Byte(), 0, 32).Draw(t, "ignoredPayload")

		var buf bytes.Buffer
		require.NoError(t, c.WriteFrame(&buf, ft, payload))
		require.Equal(t, 1, buf.Len(), "ping/pong must be a single type byte")

		gotType, gotPayload, err := c.ReadFrame(&buf)
		require.NoError(t, err)
		require.Equal(t, ft, gotType)
		require.Nil(t, gotPayload)
	})
}

// TestProperty_ServerHello_RoundTrip asserts the nonce and cert hash survive
// EncodeServerHello → DecodeServerHello, and that the encoded blob's type byte
// decodes to MsgServerHello.
func TestProperty_ServerHello_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		var nonce [32]byte
		var certHash [48]byte
		copy(nonce[:], rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(t, "nonce"))
		copy(certHash[:], rapid.SliceOfN(rapid.Byte(), 48, 48).Draw(t, "certHash"))

		enc := EncodeServerHello(nonce, certHash)

		mt, err := DecodeHandshakeType(enc)
		require.NoError(t, err)
		require.Equal(t, byte(MsgServerHello), mt)

		gotNonce, gotHash, err := DecodeServerHello(enc)
		require.NoError(t, err)
		require.Equal(t, nonce, gotNonce)
		require.Equal(t, certHash, gotHash)
	})
}

// TestProperty_HandshakeType_RoundTrip asserts EncodeHandshake tags a blob with
// a type byte that DecodeHandshakeType recovers, and that the dedicated
// AgentHello/SkipAuth encoders carry their own type bytes.
func TestProperty_HandshakeType_RoundTrip(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		msgType := rapid.SampledFrom([]byte{
			MsgServerHello, MsgAgentHello, MsgSkipAuth, MsgExpectHash,
		}).Draw(t, "msgType")
		payload := rapid.SliceOfN(rapid.Byte(), 0, 128).Draw(t, "payload")

		mt, err := DecodeHandshakeType(EncodeHandshake(msgType, payload))
		require.NoError(t, err)
		require.Equal(t, msgType, mt)

		var nonce [32]byte
		var hash [48]byte
		copy(nonce[:], rapid.SliceOfN(rapid.Byte(), 32, 32).Draw(t, "nonce"))
		copy(hash[:], rapid.SliceOfN(rapid.Byte(), 48, 48).Draw(t, "hash"))

		agentType, err := DecodeHandshakeType(EncodeAgentHello(nonce, hash))
		require.NoError(t, err)
		require.Equal(t, byte(MsgAgentHello), agentType)

		skipType, err := DecodeHandshakeType(EncodeSkipAuth(hash))
		require.NoError(t, err)
		require.Equal(t, byte(MsgSkipAuth), skipType)
	})
}

// TestProperty_HandshakeDecode_NeverPanic feeds arbitrary bytes to the handshake
// decoders; a typed error is fine, a panic is not.
func TestProperty_HandshakeDecode_NeverPanic(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		data := rapid.SliceOfN(rapid.Byte(), 0, 256).Draw(t, "data")

		_, _ = DecodeHandshakeType(data)
		_, _, _ = DecodeServerHello(data)
	})
}
