package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadOrGenerateSigningKeys_CreatesNew(t *testing.T) {
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)
	require.NotNil(t, keys)
	assert.Len(t, keys.Public, 32)
	assert.Len(t, keys.Private, 64)

	// File should exist
	_, err = os.Stat(filepath.Join(dir, "update-signing.json"))
	assert.NoError(t, err)
}

func TestLoadOrGenerateSigningKeys_ReloadsExisting(t *testing.T) {
	dir := t.TempDir()
	keys1, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	keys2, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	assert.Equal(t, keys1.Public, keys2.Public)
	assert.Equal(t, keys1.Private, keys2.Private)
}

func TestSignAndVerifyHash(t *testing.T) {
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	hash := sha256.Sum256([]byte("test binary data"))
	hashHex := hex.EncodeToString(hash[:])

	sig, err := keys.SignHash(hashHex)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	valid, err := keys.VerifyHash(hashHex, sig)
	require.NoError(t, err)
	assert.True(t, valid)
}

func TestVerifyHash_WrongData(t *testing.T) {
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	hash := sha256.Sum256([]byte("original data"))
	hashHex := hex.EncodeToString(hash[:])

	sig, err := keys.SignHash(hashHex)
	require.NoError(t, err)

	// Verify against different data
	wrongHash := sha256.Sum256([]byte("tampered data"))
	wrongHex := hex.EncodeToString(wrongHash[:])

	valid, err := keys.VerifyHash(wrongHex, sig)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestVerifyHash_WrongSignature(t *testing.T) {
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	hash := sha256.Sum256([]byte("test data"))
	hashHex := hex.EncodeToString(hash[:])

	// Use a bogus signature (valid hex, wrong value)
	badSig := hex.EncodeToString(make([]byte, 64))

	valid, err := keys.VerifyHash(hashHex, badSig)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestPublicKeyHex(t *testing.T) {
	dir := t.TempDir()
	keys, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	hexStr := keys.PublicKeyHex()
	assert.Len(t, hexStr, 64) // 32 bytes = 64 hex chars

	decoded, err := hex.DecodeString(hexStr)
	require.NoError(t, err)
	assert.Equal(t, []byte(keys.Public), decoded)
}
