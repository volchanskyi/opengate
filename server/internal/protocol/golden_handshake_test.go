package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGoldenHandshakeSkipAuth verifies the cross-language wire format of the
// 0x14 SkipAuth fast-path message: the Rust agent encodes it on reconnect and
// the Go server decodes it. The fixture carries a 0xCC-filled cached CA hash.
func TestGoldenHandshakeSkipAuth(t *testing.T) {
	data := readGolden(t, "handshake_skip_auth.bin")
	assert.Len(t, data, 49)
	assert.Equal(t, byte(MsgSkipAuth), data[0])

	var expectedHash, gotHash [48]byte
	for i := range expectedHash {
		expectedHash[i] = 0xCC
	}
	copy(gotHash[:], data[1:49])
	assert.Equal(t, expectedHash, gotHash)
}
