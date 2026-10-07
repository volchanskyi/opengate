// Package cert manages the root CA and signs agent, server and MPS certificates.
package cert

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"path/filepath"
)

// Manager handles CA and certificate operations.
type Manager struct {
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	caCertPEM []byte
}

// NewManager loads the CA files under dataDir, generating a self-signed CA when none exist.
func NewManager(dataDir string) (*Manager, error) {
	certPath := filepath.Join(dataDir, "ca.crt")
	keyPath := filepath.Join(dataDir, "ca.key")

	if fileExists(certPath) && fileExists(keyPath) {
		return loadManager(certPath, keyPath)
	}

	return generateManager(dataDir)
}

// CACert returns the parsed CA certificate.
func (m *Manager) CACert() *x509.Certificate {
	return m.caCert
}

// CACertPEM returns the CA certificate in PEM encoding.
func (m *Manager) CACertPEM() []byte {
	return m.caCertPEM
}

func (m *Manager) caPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(m.caCert)
	return pool
}

// ServerTLSConfig returns a tls.Config that requires and verifies agent client certificates.
func (m *Manager) ServerTLSConfig(extraDNS ...string) (*tls.Config, error) {
	serverCert, err := m.SignServer(extraDNS...)
	if err != nil {
		return nil, fmt.Errorf("sign server cert: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    m.caPool(),
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}, nil
}

// AgentTLSConfig returns a tls.Config for an agent to connect to the server.
func (m *Manager) AgentTLSConfig(cert *tls.Certificate) *tls.Config {
	return &tls.Config{
		ServerName:   "localhost",
		RootCAs:      m.caPool(),
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}
}

// MPSTLSConfig returns a tls.Config for the MPS server, with the TLS 1.2 minimum AMT 11.0+ speaks.
func (m *Manager) MPSTLSConfig() (*tls.Config, error) {
	mpsCert, err := m.SignMPS()
	if err != nil {
		return nil, fmt.Errorf("sign MPS cert: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*mpsCert},
		ClientAuth:   tls.NoClientCert,
		MinVersion:   tls.VersionTLS12,
	}, nil
}
