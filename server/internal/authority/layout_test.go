package authority

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
)

var now = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func parsePEM(t *testing.T, pem []byte) *x509.Certificate {
	t.Helper()
	c, err := cert.ParseCertificatePEM(pem)
	require.NoError(t, err)
	return c
}

func subject(t *testing.T, pem []byte) string {
	t.Helper()
	return parsePEM(t, pem).Subject.CommonName
}

// laidOut is the phase's files, for a layout the test expects to succeed.
func laidOut(t *testing.T, files Files, phase Phase, opts LayoutOptions) Files {
	t.Helper()
	out, err := Layout(files, phase, opts)
	require.NoError(t, err)
	return out
}

// writeFiles writes each file into dir at mode.
func writeFiles(t *testing.T, dir string, files Files, mode os.FileMode) {
	t.Helper()
	for name, data := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, mode))
	}
}

// loads writes files into a folder and starts a trust set over it, as the server would.
func loads(t *testing.T, files Files, opts ...cert.Option) (*cert.Manager, error) {
	t.Helper()
	dir := t.TempDir()
	served := Files{}
	for name, data := range files {
		if !strings.HasPrefix(name, "next-") && !strings.HasPrefix(name, "previous-") {
			served[name] = data
		}
	}
	writeFiles(t, dir, served, 0o400)
	return cert.NewManager(dir, append([]cert.Option{cert.WithClock(func() time.Time { return now })}, opts...)...)
}

func steady(t *testing.T) Files {
	t.Helper()
	files, err := Init(Files{}, now)
	require.NoError(t, err)
	return files
}

func kit(t *testing.T) Files {
	t.Helper()
	files, err := NewKit(steady(t), now)
	require.NoError(t, err)
	return files
}

func TestInitCreatesAnIssuerTheServerLoads(t *testing.T) {
	files := steady(t)
	assert.Equal(t, []string{cert.IssuerCertFile, cert.IssuerKeyFile}, files.Names())
	_, err := loads(t, files)
	assert.NoError(t, err)
}

func TestInitRefusesToOverwriteAnExistingKey(t *testing.T) {
	_, err := Init(steady(t), now)
	assert.ErrorIs(t, err, ErrKeyExists)
}

func TestNewKitStagesTheNextKeyWithoutChangingRoles(t *testing.T) {
	before := steady(t)
	files, err := NewKit(before, now)
	require.NoError(t, err)
	assert.Equal(t, before[cert.IssuerCertFile], files[cert.IssuerCertFile], "the issuer is unchanged")
	for _, name := range []string{NextCertFile, NextKeyFile, NextCrossFile} {
		assert.NotEmpty(t, files[name], name)
	}
	_, err = NewKit(files, now)
	assert.ErrorIs(t, err, ErrKitExists)
}

func TestChangeoverIssuesFromTheNextKeyAndAcceptsTheOld(t *testing.T) {
	staged := kit(t)
	old := staged[cert.IssuerCertFile]
	files := laidOut(t, staged, PhaseChangeover, LayoutOptions{})

	assert.Equal(t, "OpenGate CA (next)", subject(t, files[cert.IssuerCertFile]))
	assert.Equal(t, old, files[cert.AcceptedFile])
	assert.Equal(t, old, files[cert.MPSIssuerCertFile], "AMT keeps the key its firmware was given")
	assert.NotEmpty(t, files[cert.ChainFile])
	assert.Empty(t, files[NextKeyFile], "the kit is consumed")
	m, err := loads(t, files)
	require.NoError(t, err)
	assert.True(t, m.InChangeover())
}

func TestRetireMakesTheOldKeyRenewOnly(t *testing.T) {
	changed := laidOut(t, kit(t), PhaseChangeover, LayoutOptions{})
	files := laidOut(t, changed, PhaseRetire, LayoutOptions{})

	assert.Equal(t, changed[cert.AcceptedFile], files[cert.RenewOnlyFile])
	assert.Empty(t, files[cert.AcceptedFile])
	assert.NotEmpty(t, files[cert.MPSIssuerKeyFile], "the old key stays for AMT until it is reprovisioned")
	_, err := loads(t, files)
	assert.NoError(t, err)

	reprovisioned := laidOut(t, changed, PhaseRetire, LayoutOptions{MPS: MPSNew})
	assert.Empty(t, reprovisioned[cert.MPSIssuerKeyFile])
	assert.Empty(t, reprovisioned[PreviousKeyFile], "the old key is destroyed")
}

func TestEmergencyNeedsTheCompromiseDate(t *testing.T) {
	files := laidOut(t, kit(t), PhaseEmergency, LayoutOptions{})
	assert.NotEmpty(t, files[cert.VouchedOnlyFile])

	_, err := loads(t, files)
	assert.ErrorContains(t, err, "date of the compromise")
	_, err = loads(t, files, cert.WithCompromisedSince(now.Add(-time.Hour)))
	assert.NoError(t, err)
}

func TestSettleDropsTheOldKeyWhenAsked(t *testing.T) {
	changed := laidOut(t, kit(t), PhaseChangeover, LayoutOptions{})
	retired := laidOut(t, changed, PhaseRetire, LayoutOptions{MPS: MPSNew})

	kept := laidOut(t, retired, PhaseSettle, LayoutOptions{})
	assert.NotEmpty(t, kept[cert.RenewOnlyFile], "renew-only until the census date")
	assert.Empty(t, kept[cert.ChainFile])

	dropped := laidOut(t, retired, PhaseSettle, LayoutOptions{DropOld: true})
	assert.Equal(t, []string{cert.IssuerCertFile, cert.IssuerKeyFile}, dropped.Names())
	_, err := loads(t, dropped)
	assert.NoError(t, err)
}

func TestLayoutRefusesAPhaseItsFilesCannotReach(t *testing.T) {
	_, err := Layout(steady(t), PhaseChangeover, LayoutOptions{})
	assert.ErrorIs(t, err, ErrNoKit)
	_, err = Layout(steady(t), PhaseRetire, LayoutOptions{})
	assert.ErrorIs(t, err, ErrNotInChangeover)
	_, err = Layout(steady(t), Phase("sideways"), LayoutOptions{})
	assert.Error(t, err)
}

func TestCrossCertificateCoversTheNextKeysWholeLife(t *testing.T) {
	files := kit(t)
	next := parsePEM(t, files[NextCertFile])
	cross := parsePEM(t, files[NextCrossFile])
	old := parsePEM(t, files[cert.IssuerCertFile])
	assert.Equal(t, next.NotAfter, cross.NotAfter)
	assert.NoError(t, cross.CheckSignatureFrom(old))
	assert.Equal(t, x509.PublicKeyAlgorithm(x509.ECDSA), cross.PublicKeyAlgorithm)
}

func TestKitAndLayoutRefuseAnIssuerThatDoesNotParse(t *testing.T) {
	broken := steady(t)
	broken[cert.IssuerCertFile] = []byte("not a certificate")
	_, err := NewKit(broken, now)
	assert.Error(t, err)

	brokenKey := steady(t)
	brokenKey[cert.IssuerKeyFile] = []byte("not a key")
	_, err = NewKit(brokenKey, now)
	assert.Error(t, err)

	_, err = Layout(Files{}, PhaseSteady, LayoutOptions{})
	assert.ErrorIs(t, err, ErrNoIssuer)
}
