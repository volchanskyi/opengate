// Package updater manages agent binary updates: Ed25519 signing, manifest storage,
// and update distribution to connected agents.
package updater

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type signingKeyFile struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// SigningKeys holds an Ed25519 keypair for signing agent update manifests.
type SigningKeys struct {
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// LoadOrGenerateSigningKeys loads existing Ed25519 keys from {dataDir}/update-signing.json,
// or generates a new keypair and persists it on first run.
func LoadOrGenerateSigningKeys(dataDir string) (*SigningKeys, error) {
	path := filepath.Join(dataDir, "update-signing.json")

	data, err := os.ReadFile(path)
	if err == nil {
		var kf signingKeyFile
		if err := json.Unmarshal(data, &kf); err != nil {
			return nil, fmt.Errorf("parse update-signing.json: %w", err)
		}
		if kf.PrivateKey == "" || kf.PublicKey == "" {
			return nil, fmt.Errorf("update-signing.json has empty keys")
		}
		priv, err := hex.DecodeString(kf.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("decode private key: %w", err)
		}
		pub, err := hex.DecodeString(kf.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("decode public key: %w", err)
		}
		return &SigningKeys{Private: ed25519.PrivateKey(priv), Public: ed25519.PublicKey(pub)}, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read update-signing.json: %w", err)
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Ed25519 key: %w", err)
	}

	kf := signingKeyFile{
		PrivateKey: hex.EncodeToString(priv),
		PublicKey:  hex.EncodeToString(pub),
	}
	jsonData, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal signing keys: %w", err)
	}
	if err := os.WriteFile(path, jsonData, 0600); err != nil {
		return nil, fmt.Errorf("write update-signing.json: %w", err)
	}

	return &SigningKeys{Private: priv, Public: pub}, nil
}

// SignHash signs a hex-encoded SHA-256 hash and returns the signature as hex.
func (k *SigningKeys) SignHash(sha256Hex string) (string, error) {
	hashBytes, err := hex.DecodeString(sha256Hex)
	if err != nil {
		return "", fmt.Errorf("decode sha256 hex: %w", err)
	}
	sig := ed25519.Sign(k.Private, hashBytes)
	return hex.EncodeToString(sig), nil
}

// VerifyHash verifies a hex-encoded Ed25519 signature against a hex-encoded SHA-256 hash.
func (k *SigningKeys) VerifyHash(sha256Hex, signatureHex string) (bool, error) {
	hashBytes, err := hex.DecodeString(sha256Hex)
	if err != nil {
		return false, fmt.Errorf("decode sha256 hex: %w", err)
	}
	sigBytes, err := hex.DecodeString(signatureHex)
	if err != nil {
		return false, fmt.Errorf("decode signature hex: %w", err)
	}
	return ed25519.Verify(k.Public, hashBytes, sigBytes), nil
}

// PublicKeyHex returns the public key as a hex-encoded string.
func (k *SigningKeys) PublicKeyHex() string {
	return hex.EncodeToString(k.Public)
}
