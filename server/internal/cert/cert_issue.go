package cert

import (
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	// leafValidity is every leaf's lifetime: a machine renews from two thirds of it.
	leafValidity    = 90 * 24 * time.Hour
	clockSkewMargin = 5 * time.Minute
)

// ErrAuthorityExpired refuses an issuance past the issuing master key's own expiry.
var ErrAuthorityExpired = errors.New("the issuing master key has expired")

// leafTemplate builds a leaf valid from now for leafValidity, never past its issuer's expiry,
// returned by value so the signer keeps it on its own stack.
func leafTemplate(issuer *x509.Certificate, now time.Time, subject pkix.Name, keyUsage x509.KeyUsage, ext x509.ExtKeyUsage) (x509.Certificate, error) {
	if now.After(issuer.NotAfter) {
		return x509.Certificate{}, ErrAuthorityExpired
	}
	serial, err := randomSerial()
	if err != nil {
		return x509.Certificate{}, err
	}
	notAfter := now.Add(leafValidity)
	if notAfter.After(issuer.NotAfter) {
		notAfter = issuer.NotAfter
	}
	return x509.Certificate{
		SerialNumber: serial,
		Subject:      subject,
		NotBefore:    now.Add(-clockSkewMargin),
		NotAfter:     notAfter,
		KeyUsage:     keyUsage,
		ExtKeyUsage:  []x509.ExtKeyUsage{ext},
	}, nil
}

// issueLeaf signs template for pub with issuer, wrapping failures with what.
func issueLeaf(template *x509.Certificate, issuer *x509.Certificate, issuerKey crypto.Signer, pub crypto.PublicKey, what string) ([]byte, error) {
	certDER, err := x509.CreateCertificate(rand.Reader, template, issuer, pub, issuerKey)
	if err != nil {
		return nil, fmt.Errorf("sign %s: %w", what, err)
	}
	return certDER, nil
}

// issueTLS signs template for key and packages it with the chain that follows it.
func issueTLS(template *x509.Certificate, issuer *x509.Certificate, issuerKey crypto.Signer, key crypto.Signer, chain [][]byte, what string) (*tls.Certificate, error) {
	certDER, err := issueLeaf(template, issuer, issuerKey, key.Public(), what)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", what, err)
	}
	return &tls.Certificate{
		Certificate: append([][]byte{certDER}, chain...),
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

func randomSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generate serial: %w", err)
	}
	return serial, nil
}

// NextLeafEnd is when a leaf issued now would expire, which the issuer's own expiry can cap.
func (m *Manager) NextLeafEnd() time.Time {
	end := m.now().Add(leafValidity)
	if end.After(m.issuer.NotAfter) {
		return m.issuer.NotAfter
	}
	return end
}

// InChangeover reports whether the trust set names a master key besides the issuer, which makes
// the renewal sweep run often.
func (m *Manager) InChangeover() bool {
	for _, a := range m.authorities {
		if a.Role == RoleAccepted || a.Role == RoleRenewOnly || a.Role == RoleVouchedOnly {
			return true
		}
	}
	return false
}
