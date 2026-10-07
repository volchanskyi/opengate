package notifications

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrGenerateVAPID_GeneratesOnFirstCall(t *testing.T) {
	dir := t.TempDir()
	priv, pub, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)
	assert.NotEmpty(t, priv)
	assert.NotEmpty(t, pub)

	_, err = os.Stat(filepath.Join(dir, "vapid.json"))
	assert.NoError(t, err)
}

func TestLoadOrGenerateVAPID_LoadsExistingKeys(t *testing.T) {
	dir := t.TempDir()

	priv1, pub1, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)

	priv2, pub2, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)

	assert.Equal(t, priv1, priv2)
	assert.Equal(t, pub1, pub2)
}

func assertVAPIDFileRejected(t *testing.T, content, wantErr string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "vapid.json"), []byte(content), 0600))

	_, _, err := LoadOrGenerateVAPID(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), wantErr)
}

func TestLoadOrGenerateVAPID_CorruptFileReturnsError(t *testing.T) {
	assertVAPIDFileRejected(t, "not json", "parse vapid.json")
}

func TestLoadOrGenerateVAPID_EmptyKeysReturnsError(t *testing.T) {
	assertVAPIDFileRejected(t, `{"private_key":"","public_key":""}`, "empty keys")
}

func TestLoadOrGenerateVAPID_PrivateKeyExactly32Bytes(t *testing.T) {
	for range 10 { // multiple keys surface the rare scalar shorter than 32 bytes
		dir := t.TempDir()
		priv, _, err := LoadOrGenerateVAPID(dir)
		require.NoError(t, err)
		raw, err := base64.RawURLEncoding.DecodeString(priv)
		require.NoError(t, err)
		assert.Equal(t, 32, len(raw),
			"VAPID private key must be exactly 32 bytes, got %d (priv=%q)", len(raw), priv)
	}
}
