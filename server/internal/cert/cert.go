// Package cert manages TLS certificate lifecycle including root CA generation
// and agent certificate signing.
package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Manager handles CA and certificate operations.
type Manager struct {
	caCert    *x509.Certificate
	caKey     *ecdsa.PrivateKey
	caCertPEM []byte
}

// NewManager creates a cert manager rooted at dataDir. If CA files exist,
// they are loaded; otherwise a new self-signed CA is generated.
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

// SignAgent generates a TLS certificate for an agent, signed by the CA.
func (m *Manager) SignAgent(deviceID, hostname string) (*tls.Certificate, error) {
	if deviceID == "" {
		return nil, errors.New("device ID must not be empty")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate agent key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: deviceID},
		DNSNames:     []string{hostname},
		NotBefore:    now.Add(-5 * time.Minute), // clock skew tolerance
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign agent cert: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// SignServer generates a TLS certificate for the server, signed by the CA.
// The cert uses CN=OpenGate Server with localhost and 127.0.0.1 as SANs.
// Additional DNS names (e.g. the QUIC hostname) can be passed via extraDNS.
func (m *Manager) SignServer(extraDNS ...string) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate server key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "OpenGate Server"},
		DNSNames:     append([]string{"localhost"}, extraDNS...),
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign server cert: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// ServerTLSConfig returns a tls.Config for the server that requires
// and verifies agent client certificates. It generates a server cert
// signed by the CA for TLS handshake.
func (m *Manager) ServerTLSConfig(extraDNS ...string) (*tls.Config, error) {
	pool := x509.NewCertPool()
	pool.AddCert(m.caCert)

	serverCert, err := m.SignServer(extraDNS...)
	if err != nil {
		return nil, fmt.Errorf("sign server cert: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}, nil
}

// AgentTLSConfig returns a tls.Config for an agent to connect to the server.
func (m *Manager) AgentTLSConfig(cert *tls.Certificate) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(m.caCert)

	return &tls.Config{
		ServerName:   "localhost",
		RootCAs:      pool,
		Certificates: []tls.Certificate{*cert},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}
}

// SignMPS generates an RSA 2048 TLS certificate for the MPS server, signed
// by the CA. Intel AMT firmware requires RSA keys (not ECDSA).
func (m *Manager) SignMPS() (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate MPS key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "OpenGate MPS"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
		NotBefore:    now.Add(-5 * time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("sign MPS cert: %w", err)
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}

// MPSTLSConfig returns a tls.Config for the Intel AMT MPS server.
// It uses TLS 1.2 minimum for AMT firmware compatibility (AMT 11.0+, 2015)
// and an RSA 2048 server certificate.
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

// --- internal helpers ---

func generateManager(dataDir string) (*Manager, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate CA key: %w", err)
	}

	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "OpenGate CA"},
		NotBefore:             now.Add(-5 * time.Minute),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("create CA cert: %w", err)
	}

	caCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("marshal CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.WriteFile(filepath.Join(dataDir, "ca.crt"), certPEM, 0644); err != nil {
		return nil, fmt.Errorf("write CA cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "ca.key"), keyPEM, 0600); err != nil {
		return nil, fmt.Errorf("write CA key: %w", err)
	}

	return &Manager{caCert: caCert, caKey: key, caCertPEM: certPEM}, nil
}

func loadManager(certPath, keyPath string) (*Manager, error) {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read CA key: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return nil, errors.New("invalid CA cert PEM")
	}
	caCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, errors.New("invalid CA key PEM")
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse CA key: %w", err)
	}

	return &Manager{caCert: caCert, caKey: caKey, caCertPEM: certPEM}, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
