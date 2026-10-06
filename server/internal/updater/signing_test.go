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

func sha256Hex(data string) string {
	sum := sha256.Sum256([]byte(data))
	return hex.EncodeToString(sum[:])
}

func TestLoadOrGenerateSigningKeys_CreatesNew(t *testing.T) {
	keys, dir := setupSigningKeys(t)
	require.NotNil(t, keys)
	assert.Len(t, keys.Public, 32)
	assert.Len(t, keys.Private, 64)

	_, err := os.Stat(filepath.Join(dir, "update-signing.json"))
	assert.NoError(t, err)
}

func TestLoadOrGenerateSigningKeys_ReloadsExisting(t *testing.T) {
	keys1, dir := setupSigningKeys(t)

	keys2, err := LoadOrGenerateSigningKeys(dir)
	require.NoError(t, err)

	assert.Equal(t, keys1.Public, keys2.Public)
	assert.Equal(t, keys1.Private, keys2.Private)
}

func TestSignAndVerifyHash(t *testing.T) {
	keys, _ := setupSigningKeys(t)
	hashHex := sha256Hex("test binary data")

	sig, err := keys.SignHash(hashHex)
	require.NoError(t, err)
	assert.NotEmpty(t, sig)

	valid, err := keys.VerifyHash(hashHex, sig)
	require.NoError(t, err)
	assert.True(t, valid)
}

func assertVerifyRejects(t *testing.T, keys *SigningKeys, hashHex, sig string) {
	t.Helper()
	valid, err := keys.VerifyHash(hashHex, sig)
	require.NoError(t, err)
	assert.False(t, valid)
}

func TestVerifyHash_WrongData(t *testing.T) {
	keys, _ := setupSigningKeys(t)
	sig, err := keys.SignHash(sha256Hex("original data"))
	require.NoError(t, err)

	assertVerifyRejects(t, keys, sha256Hex("tampered data"), sig)
}

func TestVerifyHash_WrongSignature(t *testing.T) {
	keys, _ := setupSigningKeys(t)

	assertVerifyRejects(t, keys, sha256Hex("test data"), hex.EncodeToString(make([]byte, 64)))
}

func assertSigningKeysFileRejected(t *testing.T, content, wantErr string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "update-signing.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	_, err := LoadOrGenerateSigningKeys(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), wantErr)
}

func TestLoadOrGenerateSigningKeys_CorruptedFile(t *testing.T) {
	assertSigningKeysFileRejected(t, "not json", "parse update-signing.json")
}

func TestLoadOrGenerateSigningKeys_EmptyKeys(t *testing.T) {
	assertSigningKeysFileRejected(t, `{"private_key":"","public_key":""}`, "empty keys")
}

func TestSignHash_InvalidHex(t *testing.T) {
	keys, _ := setupSigningKeys(t)

	_, err := keys.SignHash("not-valid-hex!")
	assert.Error(t, err)
}

func TestVerifyHash_InvalidHex(t *testing.T) {
	keys, _ := setupSigningKeys(t)

	_, err := keys.VerifyHash("not-hex", "also-not-hex")
	assert.Error(t, err)
}

func TestPublicKeyHex(t *testing.T) {
	keys, _ := setupSigningKeys(t)

	hexStr := keys.PublicKeyHex()
	assert.Len(t, hexStr, 64)

	decoded, err := hex.DecodeString(hexStr)
	require.NoError(t, err)
	assert.Equal(t, []byte(keys.Public), decoded)
}
