package notifications

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type vapidKeys struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// LoadOrGenerateVAPID loads an existing VAPID key pair from {dataDir}/vapid.json,
// or generates a new ECDSA P-256 key pair and writes it on first run.
func LoadOrGenerateVAPID(dataDir string) (privateKey, publicKey string, err error) {
	path := filepath.Join(dataDir, "vapid.json")

	data, err := os.ReadFile(path)
	if err == nil {
		var keys vapidKeys
		if err := json.Unmarshal(data, &keys); err != nil {
			return "", "", fmt.Errorf("parse vapid.json: %w", err)
		}
		if keys.PrivateKey == "" || keys.PublicKey == "" {
			return "", "", fmt.Errorf("vapid.json has empty keys")
		}
		return keys.PrivateKey, keys.PublicKey, nil
	}
	if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("read vapid.json: %w", err)
	}

	// Generate new ECDSA P-256 key pair.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generate VAPID key: %w", err)
	}

	privBytes := key.D.Bytes()
	// Pad to 32 bytes if needed.
	for len(privBytes) < 32 {
		privBytes = append([]byte{0}, privBytes...)
	}
	privateKey = base64.RawURLEncoding.EncodeToString(privBytes)

	pubBytes := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	publicKey = base64.RawURLEncoding.EncodeToString(pubBytes)

	keys := vapidKeys{PrivateKey: privateKey, PublicKey: publicKey}
	jsonData, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("marshal vapid keys: %w", err)
	}
	if err := os.WriteFile(path, jsonData, 0600); err != nil {
		return "", "", fmt.Errorf("write vapid.json: %w", err)
	}

	return privateKey, publicKey, nil
}
