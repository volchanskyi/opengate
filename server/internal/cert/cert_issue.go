package cert

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"
)

const (
	leafValidity    = 365 * 24 * time.Hour
	clockSkewMargin = 5 * time.Minute
)

// leafTemplate builds a leaf certificate template valid for one year.
func leafTemplate(subject pkix.Name, keyUsage x509.KeyUsage, ext x509.ExtKeyUsage) (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return &x509.Certificate{
		SerialNumber: serial,
		Subject:      subject,
		NotBefore:    now.Add(-clockSkewMargin),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     keyUsage,
		ExtKeyUsage:  []x509.ExtKeyUsage{ext},
	}, nil
}

// issueLeaf signs template for pub with the CA, wrapping failures with what.
func (m *Manager) issueLeaf(template *x509.Certificate, pub crypto.PublicKey, what string) ([]byte, error) {
	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, pub, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign %s: %w", what, err)
	}
	return certDER, nil
}

// issueTLS signs template for key and packages the result as a tls.Certificate.
func (m *Manager) issueTLS(template *x509.Certificate, key crypto.Signer, what string) (*tls.Certificate, error) {
	certDER, err := m.issueLeaf(template, key.Public(), what)
	if err != nil {
		return nil, err
	}
	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}
