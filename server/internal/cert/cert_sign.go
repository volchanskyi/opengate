package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
)

// SignAgent generates a TLS certificate for an agent, signed by the CA.
func (m *Manager) SignAgent(deviceID, hostname string) (*tls.Certificate, error) {
	if deviceID == "" {
		return nil, errors.New("device ID must not be empty")
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate agent key: %w", err)
	}

	template, err := leafTemplate(pkix.Name{CommonName: deviceID}, x509.KeyUsageDigitalSignature, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, err
	}
	template.DNSNames = []string{hostname}

	return m.issueTLS(template, key, "agent cert")
}

// SignAgentCSR signs a PKCS#10 request, keeping its public key and CommonName (the device UUID).
func (m *Manager) SignAgentCSR(csrDER []byte) ([]byte, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}

	template, err := leafTemplate(csr.Subject, x509.KeyUsageDigitalSignature, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, err
	}

	return m.issueLeaf(template, csr.PublicKey, "agent CSR")
}
