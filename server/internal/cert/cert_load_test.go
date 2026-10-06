package cert

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	caCertFile = "ca.crt"
	caKeyFile  = "ca.key"
)

func TestNewManager(t *testing.T) {
	t.Run("creates CA on first init", func(t *testing.T) {
		dir := t.TempDir()
		m, err := NewManager(dir)
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(dir, caCertFile))
		assert.NoError(t, err)
		_, err = os.Stat(filepath.Join(dir, caKeyFile))
		assert.NoError(t, err)

		assert.NotNil(t, m.CACert())
	})

	t.Run("loads existing CA on subsequent init", func(t *testing.T) {
		dir := t.TempDir()
		m1, err := NewManager(dir)
		require.NoError(t, err)

		m2, err := NewManager(dir)
		require.NoError(t, err)

		assert.Equal(t, m1.CACert().SerialNumber, m2.CACert().SerialNumber)
	})

	t.Run("fails on invalid directory", func(t *testing.T) {
		_, err := NewManager("/nonexistent/path/certs")
		assert.Error(t, err)
	})

	const (
		badCertPEM = "-----BEGIN CERTIFICATE-----\nYmFkZGF0YQ==\n-----END CERTIFICATE-----\n"
		badKeyPEM  = "-----BEGIN EC PRIVATE KEY-----\nYmFkZGF0YQ==\n-----END EC PRIVATE KEY-----\n"
	)
	corrupt := []struct {
		name    string
		file    string
		content string
		perm    os.FileMode
	}{
		{"fails on corrupt cert PEM", caCertFile, "not-pem-data", 0644},
		{"fails on corrupt key PEM", caKeyFile, "not-pem-data", 0600},
		{"fails on invalid cert DER in valid PEM", caCertFile, badCertPEM, 0644},
		{"fails on invalid key DER in valid PEM", caKeyFile, badKeyPEM, 0600},
	}
	for _, tc := range corrupt {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := NewManager(dir)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, tc.file), []byte(tc.content), tc.perm))

			_, err = NewManager(dir)
			assert.Error(t, err)
		})
	}

	unreadable := []struct {
		name string
		file string
	}{
		{"fails on unreadable cert file", caCertFile},
		{"fails on unreadable key file", caKeyFile},
	}
	for _, tc := range unreadable {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			_, err := NewManager(dir)
			require.NoError(t, err)
			path := filepath.Join(dir, tc.file)
			require.NoError(t, os.Chmod(path, 0000))
			t.Cleanup(func() { os.Chmod(path, 0600) })

			_, err = NewManager(dir)
			assert.Error(t, err)
		})
	}

	t.Run("fails to write to read-only dir", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0500))
		t.Cleanup(func() { os.Chmod(dir, 0700) })

		_, err := NewManager(dir)
		assert.Error(t, err)
	})
}
