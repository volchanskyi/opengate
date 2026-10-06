package cert

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignServer(t *testing.T) {
	m := newTestManager(t)

	t.Run("default SANs include localhost", func(t *testing.T) {
		tlsCert, err := m.SignServer()
		require.NoError(t, err)

		leaf := parseLeaf(t, tlsCert)
		assert.Equal(t, "OpenGate Server", leaf.Subject.CommonName)
		assert.Contains(t, leaf.DNSNames, "localhost")
		assert.False(t, leaf.IsCA)
		verifyAgainstCA(t, m, leaf, "", x509.ExtKeyUsageServerAuth)
	})

	t.Run("extra DNS names added to SANs", func(t *testing.T) {
		tlsCert, err := m.SignServer("quic.example.com", "backup.example.com")
		require.NoError(t, err)

		leaf := parseLeaf(t, tlsCert)
		assert.Contains(t, leaf.DNSNames, "localhost")
		assert.Contains(t, leaf.DNSNames, "quic.example.com")
		assert.Contains(t, leaf.DNSNames, "backup.example.com")
		assert.Len(t, leaf.DNSNames, 3)
		verifyAgainstCA(t, m, leaf, "quic.example.com", x509.ExtKeyUsageServerAuth)
	})
}

func TestServerTLSConfig(t *testing.T) {
	cfg, err := newTestManager(t).ServerTLSConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.NotNil(t, cfg.ClientCAs)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
}

func TestSignMPS(t *testing.T) {
	m := newTestManager(t)

	t.Run("generates RSA 2048 certificate", func(t *testing.T) {
		tlsCert, err := m.SignMPS()
		require.NoError(t, err)
		assert.NotNil(t, tlsCert)

		leaf := parseLeaf(t, tlsCert)
		assert.Equal(t, "OpenGate MPS", leaf.Subject.CommonName)
		assert.Contains(t, leaf.DNSNames, "localhost")
		assert.False(t, leaf.IsCA)
		assert.Equal(t, x509.RSA, leaf.PublicKeyAlgorithm)
		verifyAgainstCA(t, m, leaf, "", x509.ExtKeyUsageServerAuth)
	})

	t.Run("each call generates unique certificate", func(t *testing.T) {
		assertUniqueSerials(t, func() *x509.Certificate { return mustSignLeaf(t, m.SignMPS) })
	})
}

func TestMPSTLSConfig(t *testing.T) {
	cfg, err := newTestManager(t).MPSTLSConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, tls.NoClientCert, cfg.ClientAuth)
	assert.Equal(t, uint16(tls.VersionTLS12), cfg.MinVersion)
	assert.Len(t, cfg.Certificates, 1)
}
