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

	// File should exist.
	_, err = os.Stat(filepath.Join(dir, "vapid.json"))
	assert.NoError(t, err)
}

func TestLoadOrGenerateVAPID_LoadsExistingKeys(t *testing.T) {
	dir := t.TempDir()

	// First call generates.
	priv1, pub1, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)

	// Second call loads the same keys.
	priv2, pub2, err := LoadOrGenerateVAPID(dir)
	require.NoError(t, err)

	assert.Equal(t, priv1, priv2)
	assert.Equal(t, pub1, pub2)
}

func TestLoadOrGenerateVAPID_CorruptFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "vapid.json"), []byte("not json"), 0600)
	require.NoError(t, err)

	_, _, err = LoadOrGenerateVAPID(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse vapid.json")
}

func TestLoadOrGenerateVAPID_EmptyKeysReturnsError(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "vapid.json"), []byte(`{"private_key":"","public_key":""}`), 0600)
	require.NoError(t, err)

	_, _, err = LoadOrGenerateVAPID(dir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty keys")
}

// TestLoadOrGenerateVAPID_PrivateKeyExactly32Bytes pins the padding loop in
// vapid.go (`for len(privBytes) < 32`). RFC 8292 requires the VAPID private
// key to be exactly 32 bytes (P-256 scalar). Without this assertion, the
// CONDITIONALS_BOUNDARY mutation `<` → `<=` survives because shorter D
// values are rare and the existing tests don't decode the key.
func TestLoadOrGenerateVAPID_PrivateKeyExactly32Bytes(t *testing.T) {
	for range 10 { // exercise multiple keys to surface the rare D.Bytes() < 32 case.
		dir := t.TempDir()
		priv, _, err := LoadOrGenerateVAPID(dir)
		require.NoError(t, err)
		raw, err := base64.RawURLEncoding.DecodeString(priv)
		require.NoError(t, err)
		assert.Equal(t, 32, len(raw),
			"VAPID private key must be exactly 32 bytes, got %d (priv=%q)", len(raw), priv)
	}
}
