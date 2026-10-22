package cert

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// CrossSign certifies the next master key's own key and subject with the current one, valid for
// the next key's whole life, so a machine trusting only the current key verifies the next.
func CrossSign(next *x509.Certificate, current *x509.Certificate, currentKey crypto.Signer) ([]byte, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               next.Subject,
		RawSubject:            next.RawSubject,
		NotBefore:             next.NotBefore,
		NotAfter:              next.NotAfter,
		KeyUsage:              next.KeyUsage,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            next.MaxPathLen,
		MaxPathLenZero:        next.MaxPathLenZero,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, current, next.PublicKey, currentKey)
	if err != nil {
		return nil, fmt.Errorf("cross-sign: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// ParseCertificatePEM reads the single certificate a PEM block holds.
func ParseCertificatePEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid certificate PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

// ParseKeyPEM reads an EC or PKCS#8 private key.
func ParseKeyPEM(data []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid key PEM")
	}
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}
	signer, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("the key cannot sign")
	}
	return signer, nil
}
