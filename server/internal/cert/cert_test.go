package cert

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	t.Run("creates CA on first init", func(t *testing.T) {
		dir := t.TempDir()
		m, err := NewManager(dir)
		require.NoError(t, err)

		// CA cert and key files should exist
		_, err = os.Stat(filepath.Join(dir, "ca.crt"))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(dir, "ca.key"))
		assert.NoError(t, err)

		// Manager should have a valid CA certificate
		assert.NotNil(t, m.CACert())
	})

	t.Run("loads existing CA on subsequent init", func(t *testing.T) {
		dir := t.TempDir()
		m1, err := NewManager(dir)
		require.NoError(t, err)
		cert1 := m1.CACert()

		m2, err := NewManager(dir)
		require.NoError(t, err)
		cert2 := m2.CACert()

		// Should load the same CA, not generate a new one
		assert.Equal(t, cert1.SerialNumber, cert2.SerialNumber)
	})

	t.Run("fails on invalid directory", func(t *testing.T) {
		_, err := NewManager("/nonexistent/path/certs")
		assert.Error(t, err)
	})

	t.Run("fails on corrupt cert PEM", func(t *testing.T) {
		dir := t.TempDir()
		// Create valid CA first to get key file
		_, err := NewManager(dir)
		require.NoError(t, err)
		// Corrupt the cert file
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.crt"), []byte("not-pem-data"), 0644))

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails on corrupt key PEM", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewManager(dir)
		require.NoError(t, err)
		// Corrupt only the key file
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.key"), []byte("not-pem-data"), 0600))

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails on invalid cert DER in valid PEM", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewManager(dir)
		require.NoError(t, err)
		// Write valid PEM wrapper but with garbage DER content
		badPEM := "-----BEGIN CERTIFICATE-----\nYmFkZGF0YQ==\n-----END CERTIFICATE-----\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.crt"), []byte(badPEM), 0644))

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails on invalid key DER in valid PEM", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewManager(dir)
		require.NoError(t, err)
		// Keep the valid cert, corrupt key with valid PEM but bad DER
		badPEM := "-----BEGIN EC PRIVATE KEY-----\nYmFkZGF0YQ==\n-----END EC PRIVATE KEY-----\n"
		require.NoError(t, os.WriteFile(filepath.Join(dir, "ca.key"), []byte(badPEM), 0600))

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails on unreadable cert file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewManager(dir)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(filepath.Join(dir, "ca.crt"), 0000))
		t.Cleanup(func() { os.Chmod(filepath.Join(dir, "ca.crt"), 0644) })

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails on unreadable key file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := NewManager(dir)
		require.NoError(t, err)
		require.NoError(t, os.Chmod(filepath.Join(dir, "ca.key"), 0000))
		t.Cleanup(func() { os.Chmod(filepath.Join(dir, "ca.key"), 0600) })

		_, err = NewManager(dir)
		assert.Error(t, err)
	})

	t.Run("fails to write to read-only dir", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0555))
		t.Cleanup(func() { os.Chmod(dir, 0755) })

		_, err := NewManager(dir)
		assert.Error(t, err)
	})
}

func TestCACert(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	require.NoError(t, err)

	ca := m.CACert()
	assert.True(t, ca.IsCA)
	assert.Equal(t, "OpenGate CA", ca.Subject.CommonName)
	assert.True(t, ca.BasicConstraintsValid)
}

func TestCACertPEM(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	require.NoError(t, err)

	pem := m.CACertPEM()
	assert.Contains(t, string(pem), "BEGIN CERTIFICATE")
	assert.Contains(t, string(pem), "END CERTIFICATE")
}

func TestSignAgent(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	require.NoError(t, err)

	t.Run("signs a valid agent certificate", func(t *testing.T) {
		tlsCert, err := m.SignAgent("device-001", "workstation.local")
		require.NoError(t, err)
		assert.NotNil(t, tlsCert)

		// Parse the leaf certificate
		leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
		require.NoError(t, err)
		assert.Equal(t, "device-001", leaf.Subject.CommonName)
		assert.Contains(t, leaf.DNSNames, "workstation.local")
		assert.False(t, leaf.IsCA)

		// Verify the cert is signed by the CA
		pool := x509.NewCertPool()
		pool.AddCert(m.CACert())
		_, err = leaf.Verify(x509.VerifyOptions{
			Roots:     pool,
			KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		assert.NoError(t, err)
	})

	t.Run("each call generates unique certificate", func(t *testing.T) {
		cert1, err := m.SignAgent("dev-1", "host1")
		require.NoError(t, err)
		cert2, err := m.SignAgent("dev-2", "host2")
		require.NoError(t, err)

		leaf1, _ := x509.ParseCertificate(cert1.Certificate[0])
		leaf2, _ := x509.ParseCertificate(cert2.Certificate[0])
		assert.NotEqual(t, leaf1.SerialNumber, leaf2.SerialNumber)
	})

	t.Run("empty device ID rejected", func(t *testing.T) {
		_, err := m.SignAgent("", "host")
		assert.Error(t, err)
	})
}

func TestServerTLSConfig(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	require.NoError(t, err)

	cfg, err := m.ServerTLSConfig()
	require.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
	assert.NotNil(t, cfg.ClientCAs)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
}

func TestAgentTLSConfig(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir)
	require.NoError(t, err)

	agentCert, err := m.SignAgent("agent-tls", "agent.local")
	require.NoError(t, err)

	cfg := m.AgentTLSConfig(agentCert)
	assert.NotNil(t, cfg)
	assert.NotNil(t, cfg.RootCAs)
	assert.Len(t, cfg.Certificates, 1)
}
