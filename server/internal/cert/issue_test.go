package cert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeafNeverOutlivesItsAuthority(t *testing.T) {
	clock := newTestClock()
	dir := t.TempDir()
	authority, _ := writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "short-lived",
		clock.Now().Add(-day), clock.Now().Add(30*day))
	m := mustManager(t, dir, WithClock(clock.Now))

	leaf := issueDevice(t, m, uuid.New())
	assert.Equal(t, authority.NotAfter, leaf.NotAfter, "the leaf ends with its master key")

	server := mustSignLeaf(t, func() (*tls.Certificate, error) { return m.SignServer() })
	assert.False(t, server.NotAfter.After(authority.NotAfter))
}

func TestIssuanceRefusedAfterAuthorityExpiry(t *testing.T) {
	clock := newTestClock()
	dir := t.TempDir()
	writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "short-lived", clock.Now().Add(-day), clock.Now().Add(day))
	m := mustManager(t, dir, WithClock(clock.Now))

	clock.Advance(2 * day)
	_, err := m.IssueDevice(deviceRequest(t, ""), uuid.New())
	assert.ErrorIs(t, err, ErrAuthorityExpired)
	_, err = m.SignServer()
	assert.ErrorIs(t, err, ErrAuthorityExpired)
}

func TestDeviceIssueForcesCNAndDropsCSRExtensions(t *testing.T) {
	m := newTestManager(t)
	csr := requestFrom(t, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: uuid.NewString(), Organization: []string{"someone else"}},
		DNSNames: []string{"bank.example"},
		ExtraExtensions: []pkix.Extension{{
			Id: asn1.ObjectIdentifier{2, 5, 29, 19}, Critical: true, Value: []byte{0x30, 0x03, 0x01, 0x01, 0xff},
		}},
	}, newKey(t))

	id := uuid.New()
	issued, err := m.IssueDevice(csr, id)
	require.NoError(t, err)
	leaf := parseDER(t, issued)

	assert.Equal(t, id.String(), leaf.Subject.CommonName)
	assert.Empty(t, leaf.Subject.Organization)
	assert.Empty(t, leaf.DNSNames)
	assert.False(t, leaf.IsCA, "the request asked to be an authority")
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)
	verifyAgainstCA(t, m, leaf, "", x509.ExtKeyUsageClientAuth)
}

func TestDeviceIssueRefusesNonP256(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	require.NoError(t, err)
	for name, key := range map[string]any{"RSA": rsaKey, "P-384": p384} {
		der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
		require.NoError(t, err)
		_, err = ParseDeviceRequest(der)
		assert.ErrorIs(t, err, ErrNotP256, name)
	}
	_, err = ParseDeviceRequest([]byte("not a request"))
	assert.ErrorContains(t, err, "parse CSR")
}

func TestServerCertificateRefreshedBeforeExpiry(t *testing.T) {
	clock := newTestClock()
	m, _ := newClockedManager(t, clock)
	cfg, err := m.ServerTLSConfig()
	require.NoError(t, err)
	served := func() *tls.Certificate {
		c, err := cfg.GetCertificate(nil)
		require.NoError(t, err)
		return c
	}

	first := served()
	clock.Advance(leafValidity - serverRefreshWindow - day)
	assert.Same(t, first, served(), "a certificate with more than 30 days left is served as is")

	clock.Advance(2 * day)
	fresh := served()
	assert.NotSame(t, first, fresh)
	assert.True(t, fresh.Leaf.NotAfter.After(first.Leaf.NotAfter))
}
