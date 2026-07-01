package protocol

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func goldenDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "testdata", "golden")
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join(goldenDir(), name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "Golden file %s not found. Run Rust golden generator first.", name)
	return data
}

func TestGoldenPingPong(t *testing.T) {
	ping := readGolden(t, "ping.bin")
	assert.Equal(t, []byte{FramePing}, ping)

	pong := readGolden(t, "pong.bin")
	assert.Equal(t, []byte{FramePong}, pong)
}

func TestGoldenHandshakeServerHello(t *testing.T) {
	data := readGolden(t, "handshake_server_hello.bin")
	assert.Len(t, data, 81)
	assert.Equal(t, byte(MsgServerHello), data[0])

	nonce, certHash, err := DecodeServerHello(data)
	require.NoError(t, err)

	var expectedNonce [32]byte
	var expectedHash [48]byte
	for i := range expectedNonce {
		expectedNonce[i] = 0xAA
	}
	for i := range expectedHash {
		expectedHash[i] = 0xBB
	}
	assert.Equal(t, expectedNonce, nonce)
	assert.Equal(t, expectedHash, certHash)
}
