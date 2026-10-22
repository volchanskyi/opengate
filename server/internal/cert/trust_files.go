package cert

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// loadKeyPair reads a certificate and its key, refusing a key that does not match or that group
// or others can read.
func loadKeyPair(certPath, keyPath string) (*x509.Certificate, crypto.Signer, error) {
	certs, err := loadCerts(certPath)
	if err != nil {
		return nil, nil, err
	}
	if len(certs) != 1 {
		return nil, nil, fmt.Errorf("%s holds %d certificates, one expected", filepath.Base(certPath), len(certs))
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read CA key: %w", err)
	}
	if info.Mode().Perm()&keyFileForbiddenBit != 0 {
		return nil, nil, fmt.Errorf("%s is readable beyond its owner (mode %o)", filepath.Base(keyPath), info.Mode().Perm())
	}
	key, err := loadKey(keyPath)
	if err != nil {
		return nil, nil, err
	}
	if !publicKeysEqual(certs[0].PublicKey, key.Public()) {
		return nil, nil, fmt.Errorf("%s does not match %s", filepath.Base(keyPath), filepath.Base(certPath))
	}
	return certs[0], key, nil
}

func loadOptionalCerts(path string) ([]*x509.Certificate, error) {
	if !fileExists(path) {
		return nil, nil
	}
	return loadCerts(path)
}

// loadCerts reads every certificate in a PEM file; one block that fails to parse fails the file.
func loadCerts(path string) ([]*x509.Certificate, error) {
	// #nosec G304 -- the path is a role file under the operator's authority directory.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse CA cert in %s: %w", filepath.Base(path), err)
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("invalid CA cert PEM in %s", filepath.Base(path))
	}
	return certs, nil
}

func loadKey(path string) (crypto.Signer, error) {
	// #nosec G304 -- the path is a role file under the operator's authority directory.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read CA key: %w", err)
	}
	key, err := ParseKeyPEM(data)
	if err != nil {
		return nil, fmt.Errorf("CA key: %w", err)
	}
	return key, nil
}

// publicKeysEqual compares two public keys through the Equal method every stdlib key type has.
func publicKeysEqual(a, b crypto.PublicKey) bool {
	eq, ok := a.(interface{ Equal(crypto.PublicKey) bool })
	return ok && eq.Equal(b)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
