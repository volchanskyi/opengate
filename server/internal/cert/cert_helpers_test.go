package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m, err := NewManager(t.TempDir())
	require.NoError(t, err)
	return m
}

func parseLeaf(t *testing.T, tlsCert *tls.Certificate) *x509.Certificate {
	t.Helper()
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	require.NoError(t, err)
	return leaf
}

func verifyAgainstCA(t *testing.T, m *Manager, leaf *x509.Certificate, dnsName string, usage x509.ExtKeyUsage) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(m.CACert())
	_, err := leaf.Verify(x509.VerifyOptions{
		DNSName:   dnsName,
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{usage},
	})
	assert.NoError(t, err)
}

func assertUniqueSerials(t *testing.T, sign func() *x509.Certificate) {
	t.Helper()
	assert.NotEqual(t, sign().SerialNumber, sign().SerialNumber)
}

func mustSignLeaf(t *testing.T, sign func() (*tls.Certificate, error)) *x509.Certificate {
	t.Helper()
	c, err := sign()
	require.NoError(t, err)
	return parseLeaf(t, c)
}

func mustSignCSR(t *testing.T, m *Manager, cn string) *x509.Certificate {
	t.Helper()
	der, err := m.SignAgentCSR(makeCSR(t, cn))
	require.NoError(t, err)
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return leaf
}

func makeCSR(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	require.NoError(t, err)
	return csrDER
}
