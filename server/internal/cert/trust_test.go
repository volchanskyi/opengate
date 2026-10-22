package cert

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// changeoverLayout is the changeover phase: B issues, A is accepted, and B cross-signed by A
// follows the server's own certificate.
type changeoverLayout struct {
	dir  string
	a, b *x509.Certificate
	aKey crypto.Signer
}

func changeover(t *testing.T, clock *testClock) changeoverLayout {
	t.Helper()
	dir := t.TempDir()
	a, aKey := writeAuthority(t, t.TempDir(), "a.crt", "a.key", "Authority A",
		clock.Now().Add(-365*day), clock.Now().Add(365*day))
	b, _ := writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "Authority B",
		clock.Now().Add(-day), clock.Now().Add(3650*day))
	appendPEM(t, filepath.Join(dir, AcceptedFile), "CERTIFICATE", a.Raw)
	cross, err := CrossSign(b, a, aKey)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ChainFile), cross, 0o600))
	return changeoverLayout{dir: dir, a: a, b: b, aKey: aKey}
}

// bundleSubjects names every certificate in a PEM bundle, in order.
func bundleSubjects(t *testing.T, bundle []byte) []string {
	t.Helper()
	var names []string
	for {
		var block *pem.Block
		block, bundle = pem.Decode(bundle)
		if block == nil {
			return names
		}
		names = append(names, parseDER(t, block.Bytes).Subject.CommonName)
	}
}

func TestRoleFilesBuildTrustSet(t *testing.T) {
	clock := newTestClock()
	dir := t.TempDir()
	now := clock.Now()
	writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "issuer", now.Add(-day), now.Add(3650*day))
	writeAuthority(t, dir, AcceptedFile, "", "accepted", now.Add(-day), now.Add(3650*day))
	writeAuthority(t, dir, RenewOnlyFile, "", "renew-only", now.Add(-day), now.Add(3650*day))
	writeAuthority(t, dir, VouchedOnlyFile, "", "vouched-only", now.Add(-day), now.Add(3650*day))

	m := mustManager(t, dir, WithClock(clock.Now), WithCompromisedSince(now.Add(-time.Hour)))

	roles := map[string]Role{}
	for _, a := range m.Authorities() {
		roles[a.Cert.Subject.CommonName] = a.Role
	}
	assert.Equal(t, map[string]Role{
		"issuer": RoleIssuer, "accepted": RoleAccepted, "renew-only": RoleRenewOnly, "vouched-only": RoleVouchedOnly,
	}, roles)
	assert.Len(t, m.TrustedHashes(), 4)
}

func TestBundleIsIssuingPlusAccepted(t *testing.T) {
	clock := newTestClock()
	layout := changeover(t, clock)
	writeAuthority(t, layout.dir, RenewOnlyFile, "", "retired", clock.Now().Add(-day), clock.Now().Add(day))

	m := mustManager(t, layout.dir, WithClock(clock.Now))

	assert.Equal(t, []string{"Authority B", "Authority A"}, bundleSubjects(t, m.CACertPEM()))
	assert.Equal(t, layout.b.Raw, m.CACert().Raw, "B issues")
}

func TestStartupRefusesInconsistentRoles(t *testing.T) {
	clock := newTestClock()
	now := clock.Now()

	t.Run("a key that does not match its certificate", func(t *testing.T) {
		dir := t.TempDir()
		writeAuthority(t, dir, IssuerCertFile, "", "issuer", now.Add(-day), now.Add(day*3650))
		other := t.TempDir()
		writeAuthority(t, other, "o.crt", "o.key", "other", now.Add(-day), now.Add(day))
		data, err := os.ReadFile(filepath.Join(other, "o.key"))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, IssuerKeyFile), data, 0o600))
		_, err = NewManager(dir, WithClock(clock.Now))
		assert.ErrorContains(t, err, "does not match")
	})

	t.Run("a certificate holding two roles", func(t *testing.T) {
		dir := t.TempDir()
		issuer, _ := writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "issuer", now.Add(-day), now.Add(day*3650))
		appendPEM(t, filepath.Join(dir, AcceptedFile), "CERTIFICATE", issuer.Raw)
		_, err := NewManager(dir, WithClock(clock.Now))
		assert.ErrorContains(t, err, "two roles")
	})

	t.Run("a chain entry that does not verify", func(t *testing.T) {
		layout := changeover(t, clock)
		require.NoError(t, os.Remove(filepath.Join(layout.dir, AcceptedFile)))
		_, err := NewManager(layout.dir, WithClock(clock.Now))
		assert.ErrorContains(t, err, "does not verify")
	})

	t.Run("a vouched-only key without the date of the compromise", func(t *testing.T) {
		dir := t.TempDir()
		writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "issuer", now.Add(-day), now.Add(day*3650))
		writeAuthority(t, dir, VouchedOnlyFile, "", "leaked", now.Add(-day), now.Add(day*3650))
		_, err := NewManager(dir, WithClock(clock.Now))
		assert.ErrorContains(t, err, "date of the compromise")
	})
}

func TestMustExistNeverGeneratesCA(t *testing.T) {
	dir := t.TempDir()
	_, err := NewManager(dir, WithMustExist())
	require.Error(t, err)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a missing master key was generated")
}

func TestStartupRefusesAKeyFileOthersCanRead(t *testing.T) {
	dir := t.TempDir()
	mustManager(t, dir)
	for _, mode := range []os.FileMode{0o640, 0o604, 0o644} {
		require.NoError(t, os.Chmod(filepath.Join(dir, IssuerKeyFile), mode))
		_, err := NewManager(dir)
		assert.ErrorContains(t, err, "readable beyond its owner", "mode %o", mode)
	}
	require.NoError(t, os.Chmod(filepath.Join(dir, IssuerKeyFile), 0o400))
	_, err := NewManager(dir)
	assert.NoError(t, err)
}

func TestServerPresentsCrossChain(t *testing.T) {
	clock := newTestClock()
	layout := changeover(t, clock)
	m := mustManager(t, layout.dir, WithClock(clock.Now))

	served, err := m.SignServer("server.example")
	require.NoError(t, err)
	require.Len(t, served.Certificate, 2, "the leaf and the cross-signed issuer")
	leaf := parseLeaf(t, served)
	cross := parseDER(t, served.Certificate[1])

	for name, root := range map[string]*x509.Certificate{"only A": layout.a, "only B": layout.b} {
		roots := x509.NewCertPool()
		roots.AddCert(root)
		intermediates := x509.NewCertPool()
		intermediates.AddCert(cross)
		_, err := leaf.Verify(x509.VerifyOptions{
			DNSName: "server.example", Roots: roots, Intermediates: intermediates, CurrentTime: clock.Now(),
		})
		assert.NoError(t, err, "a machine trusting %s verifies the server", name)
	}
}

func TestMPSSignedByMPSAuthority(t *testing.T) {
	clock := newTestClock()
	dir := t.TempDir()
	writeAuthority(t, dir, IssuerCertFile, IssuerKeyFile, "issuer", clock.Now().Add(-day), clock.Now().Add(3650*day))
	mps, _ := writeAuthority(t, dir, MPSIssuerCertFile, MPSIssuerKeyFile, "AMT root",
		clock.Now().Add(-day), clock.Now().Add(3650*day))
	m := mustManager(t, dir, WithClock(clock.Now))

	leaf := mustSignLeaf(t, m.SignMPS)
	assert.NoError(t, leaf.CheckSignatureFrom(mps))
	assert.Error(t, leaf.CheckSignatureFrom(m.CACert()))
}

func TestRetiredIssuerIsRestricted(t *testing.T) {
	clock := newTestClock()
	layout := changeover(t, clock)
	leaf := signLeaf(t, clientTemplate(clock.Now(), uuid.NewString()), layout.a, layout.aKey)

	require.NoError(t, os.Remove(filepath.Join(layout.dir, AcceptedFile)))
	appendPEM(t, filepath.Join(layout.dir, RenewOnlyFile), "CERTIFICATE", layout.a.Raw)
	m := mustManager(t, layout.dir, WithClock(clock.Now))

	got := classifyOne(t, m, leaf)
	assert.Equal(t, StandingRenewOnly, got.Standing)
	assert.Equal(t, RoleRenewOnly, got.Authority.Role)
}

func TestVouchedOnlyMasterKeyNeedsAVoucher(t *testing.T) {
	clock := newTestClock()
	layout := changeover(t, clock)
	leaf := signLeaf(t, clientTemplate(clock.Now(), uuid.NewString()), layout.a, layout.aKey)

	require.NoError(t, os.Remove(filepath.Join(layout.dir, AcceptedFile)))
	appendPEM(t, filepath.Join(layout.dir, VouchedOnlyFile), "CERTIFICATE", layout.a.Raw)
	m := mustManager(t, layout.dir, WithClock(clock.Now), WithCompromisedSince(clock.Now().Add(-time.Hour)))

	got := classifyOne(t, m, leaf)
	assert.Equal(t, StandingNeedsVoucher, got.Standing)
}
