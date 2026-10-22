package cert

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const day = 24 * time.Hour

func TestClassify(t *testing.T) {
	type outcome struct {
		standing Standing
		refused  bool
		expired  bool
	}
	cases := []struct {
		name  string
		build func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate
		want  outcome
	}{
		{"valid", func(t *testing.T, m *Manager, _ *testClock) []*x509.Certificate {
			return []*x509.Certificate{issueDevice(t, m, uuid.New())}
		}, outcome{standing: StandingFull}},
		{"expired 1 day", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			leaf := issueDevice(t, m, uuid.New())
			clock.Advance(leafValidity + day)
			return []*x509.Certificate{leaf}
		}, outcome{standing: StandingRenewOnly, expired: true}},
		{"expired 90 days, the last day of the window", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			leaf := issueDevice(t, m, uuid.New())
			clock.Advance(leafValidity - clockSkewMargin + 90*day)
			return []*x509.Certificate{leaf}
		}, outcome{standing: StandingRenewOnly, expired: true}},
		{"expired 91 days", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			leaf := issueDevice(t, m, uuid.New())
			clock.Advance(leafValidity + 91*day)
			return []*x509.Certificate{leaf}
		}, outcome{standing: StandingTooOld, expired: true}},
		{"not yet valid", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			leaf := issueDevice(t, m, uuid.New())
			clock.Advance(-time.Hour)
			return []*x509.Certificate{leaf}
		}, outcome{refused: true}},
		{"foreign authority", func(t *testing.T, _ *Manager, clock *testClock) []*x509.Certificate {
			other, _ := newClockedManager(t, clock)
			return []*x509.Certificate{issueDevice(t, other, uuid.New())}
		}, outcome{refused: true}},
		{"self-signed", func(t *testing.T, _ *Manager, clock *testClock) []*x509.Certificate {
			key := newKey(t)
			template := clientTemplate(clock.Now(), uuid.NewString())
			template.SerialNumber, _ = randomSerial()
			return []*x509.Certificate{createCert(t, template, template, &key.PublicKey, key)}
		}, outcome{refused: true}},
		{"server usage presented as client", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			template := clientTemplate(clock.Now(), uuid.NewString())
			template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
			return []*x509.Certificate{signLeaf(t, template, m.issuer, m.issuerKey)}
		}, outcome{refused: true}},
		{"non-UUID common name", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			return []*x509.Certificate{signLeaf(t, clientTemplate(clock.Now(), "workstation"), m.issuer, m.issuerKey)}
		}, outcome{refused: true}},
		{"smuggled intermediate", func(t *testing.T, m *Manager, clock *testClock) []*x509.Certificate {
			key := newKey(t)
			template, err := authorityTemplate("intermediate", clock.Now())
			require.NoError(t, err)
			intermediate := createCert(t, template, m.issuer, &key.PublicKey, m.issuerKey)
			leaf := signLeaf(t, clientTemplate(clock.Now(), uuid.NewString()), intermediate, key)
			return []*x509.Certificate{leaf, intermediate}
		}, outcome{refused: true}},
		{"no certificate", func(*testing.T, *Manager, *testClock) []*x509.Certificate {
			return nil
		}, outcome{refused: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clock := newTestClock()
			m, _ := newClockedManager(t, clock)
			peer := tc.build(t, m, clock)

			got, err := m.Classify(peer)
			if tc.want.refused {
				require.ErrorIs(t, err, ErrUnacceptable)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want.standing, got.Standing)
			assert.Equal(t, tc.want.expired, got.Expired)
			assert.Equal(t, RoleIssuer, got.Authority.Role)
		})
	}
}

func TestLeafIssuedJustBeforeItsAuthorityExpiredIsRenewOnlyOnceItDoes(t *testing.T) {
	clock := newTestClock()
	dir := t.TempDir()
	writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "short-lived", clock.Now().Add(-day), clock.Now().Add(10*day))
	m := mustManager(t, dir, WithClock(clock.Now))

	clock.Advance(9 * day)
	leaf := issueDevice(t, m, uuid.New())
	assert.Equal(t, StandingFull, classifyOne(t, m, leaf).Standing)

	clock.Advance(2 * day)
	got := classifyOne(t, m, leaf)
	assert.Equal(t, StandingRenewOnly, got.Standing, "the master key's expiry ends the leaf's full standing")
	assert.True(t, got.Expired)
}

func clientTemplate(now time.Time, cn string) *x509.Certificate {
	return &x509.Certificate{
		Subject:     pkix.Name{CommonName: cn},
		NotBefore:   now.Add(-clockSkewMargin),
		NotAfter:    now.Add(leafValidity),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
}

func FuzzClassify(f *testing.F) {
	clock := newTestClock()
	dir := f.TempDir()
	m := mustManager(f, dir, WithClock(clock.Now))
	f.Add(issueDevice(f, m, uuid.New()).Raw)
	f.Add(m.CACert().Raw)
	f.Add([]byte{0x30, 0x03, 0x02, 0x01, 0x00})

	f.Fuzz(func(t *testing.T, der []byte) {
		leaf, err := x509.ParseCertificate(der)
		if err != nil {
			return
		}
		got, err := m.Classify([]*x509.Certificate{leaf})
		if err != nil {
			return
		}
		// Anything admitted names a device and verifies against the trusted master key.
		if _, perr := uuid.Parse(leaf.Subject.CommonName); perr != nil {
			t.Fatalf("admitted a certificate naming %q", leaf.Subject.CommonName)
		}
		if leaf.CheckSignatureFrom(got.Authority.Cert) != nil {
			t.Fatalf("admitted a certificate %s did not sign", got.Authority.Cert.Subject)
		}
	})
}

func BenchmarkClassify(b *testing.B) {
	clock := newTestClock()
	m := mustManager(b, b.TempDir(), WithClock(clock.Now))
	peer := []*x509.Certificate{issueDevice(b, m, uuid.New())}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Classify(peer); err != nil {
			b.Fatal(err)
		}
	}
}
