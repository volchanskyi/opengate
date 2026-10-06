package cert

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCertValidityPeriods(t *testing.T) {
	before := time.Now()
	m := newTestManager(t)
	after := time.Now()

	assertValidity := func(t *testing.T, label string, leaf *x509.Certificate, before, after time.Time, skew, duration time.Duration) {
		t.Helper()
		minNB := before.Add(-skew - time.Second)
		maxNB := after.Add(-skew + time.Second)
		assert.Falsef(t, leaf.NotBefore.Before(minNB), "%s NotBefore=%s before earliest=%s", label, leaf.NotBefore, minNB)
		assert.Falsef(t, leaf.NotBefore.After(maxNB), "%s NotBefore=%s after latest=%s", label, leaf.NotBefore, maxNB)

		minNA := before.Add(duration - time.Second)
		maxNA := after.Add(duration + time.Second)
		assert.Falsef(t, leaf.NotAfter.Before(minNA), "%s NotAfter=%s before earliest=%s", label, leaf.NotAfter, minNA)
		assert.Falsef(t, leaf.NotAfter.After(maxNA), "%s NotAfter=%s after latest=%s", label, leaf.NotAfter, maxNA)
	}

	const skew = 5 * time.Minute
	const leafLife = 365 * 24 * time.Hour
	assertValidity(t, "CA", m.CACert(), before, after, skew, 10*leafLife)

	signers := []struct {
		label string
		sign  func() *x509.Certificate
	}{
		{"Agent", func() *x509.Certificate {
			return mustSignLeaf(t, func() (*tls.Certificate, error) { return m.SignAgent("device-validity", "host.local") })
		}},
		{"Server", func() *x509.Certificate {
			return mustSignLeaf(t, func() (*tls.Certificate, error) { return m.SignServer() })
		}},
		{"MPS", func() *x509.Certificate { return mustSignLeaf(t, m.SignMPS) }},
		{"AgentCSR", func() *x509.Certificate { return mustSignCSR(t, m, "csr-validity") }},
	}
	for _, s := range signers {
		beforeSign := time.Now()
		leaf := s.sign()
		afterSign := time.Now()
		assertValidity(t, s.label, leaf, beforeSign, afterSign, skew, leafLife)
	}
}
