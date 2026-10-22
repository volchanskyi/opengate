package cert

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testClock is the server clock a test moves by hand.
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func newTestClock() *testClock {
	return &testClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// mustManager opens the manager over dir.
func mustManager(t testing.TB, dir string, opts ...Option) *Manager {
	t.Helper()
	m, err := NewManager(dir, opts...)
	require.NoError(t, err)
	return m
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return mustManager(t, t.TempDir())
}

func newClockedManager(t *testing.T, clock *testClock, opts ...Option) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	return mustManager(t, dir, append([]Option{WithClock(clock.Now)}, opts...)...), dir
}

// classifyOne classifies one presented certificate the manager accepts.
func classifyOne(t *testing.T, m *Manager, leaf *x509.Certificate) Classification {
	t.Helper()
	got, err := m.Classify([]*x509.Certificate{leaf})
	require.NoError(t, err)
	return got
}

func parseLeaf(t *testing.T, tlsCert *tls.Certificate) *x509.Certificate {
	t.Helper()
	return parseDER(t, tlsCert.Certificate[0])
}

func verifyAgainstCA(t *testing.T, m *Manager, leaf *x509.Certificate, dnsName string, usage x509.ExtKeyUsage) {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(m.CACert())
	_, err := leaf.Verify(x509.VerifyOptions{
		DNSName:     dnsName,
		Roots:       pool,
		CurrentTime: m.Now(),
		KeyUsages:   []x509.ExtKeyUsage{usage},
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

// deviceRequest builds a P-256 request naming cn, signed by a fresh key.
func deviceRequest(t testing.TB, cn string) *x509.CertificateRequest {
	t.Helper()
	return requestFrom(t, &x509.CertificateRequest{Subject: pkix.Name{CommonName: cn}}, newKey(t))
}

// issueDevice issues a certificate for a fresh key and returns it parsed.
func issueDevice(t testing.TB, m *Manager, id uuid.UUID) *x509.Certificate {
	t.Helper()
	der, err := m.IssueDevice(deviceRequest(t, ""), id)
	require.NoError(t, err)
	return parseDER(t, der)
}
