package cert

import (
	"crypto/tls"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCACert(t *testing.T) {
	ca := newTestManager(t).CACert()
	assert.True(t, ca.IsCA)
	assert.Equal(t, "OpenGate CA", ca.Subject.CommonName)
	assert.True(t, ca.BasicConstraintsValid)
}

func TestCACertPEM(t *testing.T) {
	pem := newTestManager(t).CACertPEM()
	assert.Contains(t, string(pem), "BEGIN CERTIFICATE")
	assert.Contains(t, string(pem), "END CERTIFICATE")
}

func TestSignAgent(t *testing.T) {
	m := newTestManager(t)

	t.Run("signs a valid agent certificate", func(t *testing.T) {
		tlsCert, err := m.SignAgent("device-001", "workstation.local")
		require.NoError(t, err)
		assert.NotNil(t, tlsCert)

		leaf := parseLeaf(t, tlsCert)
		assert.Equal(t, "device-001", leaf.Subject.CommonName)
		assert.Contains(t, leaf.DNSNames, "workstation.local")
		assert.False(t, leaf.IsCA)
		verifyAgainstCA(t, m, leaf, "", x509.ExtKeyUsageClientAuth)
	})

	t.Run("each call generates unique certificate", func(t *testing.T) {
		assertUniqueSerials(t, func() *x509.Certificate {
			return mustSignLeaf(t, func() (*tls.Certificate, error) { return m.SignAgent("dev-1", "host1") })
		})
	})

	t.Run("empty device ID rejected", func(t *testing.T) {
		_, err := m.SignAgent("", "host")
		assert.Error(t, err)
	})
}

func TestSignAgentCSR(t *testing.T) {
	m := newTestManager(t)

	t.Run("signs valid CSR", func(t *testing.T) {
		leaf := mustSignCSR(t, m, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
		assert.Equal(t, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", leaf.Subject.CommonName)
		assert.False(t, leaf.IsCA)
		verifyAgainstCA(t, m, leaf, "", x509.ExtKeyUsageClientAuth)
	})

	t.Run("rejects invalid CSR DER", func(t *testing.T) {
		_, err := m.SignAgentCSR([]byte("bad-data"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parse CSR")
	})

	t.Run("each CSR gets unique serial", func(t *testing.T) {
		assertUniqueSerials(t, func() *x509.Certificate { return mustSignCSR(t, m, "dev-1") })
	})
}

func TestAgentTLSConfig(t *testing.T) {
	m := newTestManager(t)

	agentCert, err := m.SignAgent("agent-tls", "agent.local")
	require.NoError(t, err)

	cfg := m.AgentTLSConfig(agentCert)
	assert.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
	assert.Len(t, cfg.Certificates, 1)
}
