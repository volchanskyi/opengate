package cert

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
