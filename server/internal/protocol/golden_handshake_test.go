package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
