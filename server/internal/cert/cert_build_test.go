package cert

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// newKey is a fresh P-256 key.
func newKey(t testing.TB) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

// parseDER parses one certificate.
func parseDER(t testing.TB, der []byte) *x509.Certificate {
	t.Helper()
	c, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return c
}

// createCert signs template for pub with signer, under parent, and returns it parsed.
func createCert(t testing.TB, template, parent *x509.Certificate, pub any, signer crypto.Signer) *x509.Certificate {
	t.Helper()
	der, err := x509.CreateCertificate(rand.Reader, template, parent, pub, signer)
	require.NoError(t, err)
	return parseDER(t, der)
}

// requestFrom is the device request a key signs over template.
func requestFrom(t testing.TB, template *x509.CertificateRequest, key crypto.Signer) *x509.CertificateRequest {
	t.Helper()
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	require.NoError(t, err)
	csr, err := ParseDeviceRequest(der)
	require.NoError(t, err)
	return csr
}

// writeAuthority writes a master key valid between notBefore and notAfter into dir under the
// given role file, with its key beside it when keyFile is set.
func writeAuthority(t *testing.T, dir, certFile, keyFile, cn string, notBefore, notAfter time.Time) (*x509.Certificate, crypto.Signer) {
	t.Helper()
	key := newKey(t)
	template, err := authorityTemplate(cn, notBefore)
	require.NoError(t, err)
	template.NotBefore = notBefore
	template.NotAfter = notAfter
	parsed := createCert(t, template, template, &key.PublicKey, key)
	appendPEM(t, filepath.Join(dir, certFile), "CERTIFICATE", parsed.Raw)
	if keyFile != "" {
		keyDER, err := x509.MarshalECPrivateKey(key)
		require.NoError(t, err)
		appendPEM(t, filepath.Join(dir, keyFile), "EC PRIVATE KEY", keyDER)
	}
	return parsed, key
}

func appendPEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer f.Close()
	require.NoError(t, pem.Encode(f, &pem.Block{Type: kind, Bytes: der}))
}

// signLeaf signs an arbitrary leaf template with an issuer, for the cases the manager refuses to
// mint itself.
func signLeaf(t *testing.T, template *x509.Certificate, issuer *x509.Certificate, issuerKey crypto.Signer) *x509.Certificate {
	t.Helper()
	if template.SerialNumber == nil {
		serial, err := randomSerial()
		require.NoError(t, err)
		template.SerialNumber = serial
	}
	return createCert(t, template, issuer, &newKey(t).PublicKey, issuerKey)
}
