package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrNotP256 refuses a request for any key other than ECDSA P-256.
var ErrNotP256 = errors.New("the request's key is not ECDSA P-256")

// ParseDeviceRequest parses a DER certificate request and checks its signature and key type.
func ParseDeviceRequest(csrDER []byte) (*x509.CertificateRequest, error) {
	csr, err := x509.ParseCertificateRequest(csrDER)
	if err != nil {
		return nil, fmt.Errorf("parse CSR: %w", err)
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, fmt.Errorf("invalid CSR signature: %w", err)
	}
	pub, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, ErrNotP256
	}
	return csr, nil
}

// IssueDevice signs a certificate for the request's key naming deviceID, from a fresh template:
// the request's subject and extensions are ignored.
func (m *Manager) IssueDevice(csr *x509.CertificateRequest, deviceID uuid.UUID) ([]byte, error) {
	template, err := leafTemplate(m.issuer, m.now(), pkix.Name{CommonName: deviceID.String()},
		x509.KeyUsageDigitalSignature, x509.ExtKeyUsageClientAuth)
	if err != nil {
		return nil, err
	}
	return issueLeaf(&template, m.issuer, m.issuerKey, csr.PublicKey, "device cert")
}
