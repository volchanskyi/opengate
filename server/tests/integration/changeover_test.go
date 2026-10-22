package integration

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

func TestOfflineAcrossRoutineChangeoverRenewsOnReturn(t *testing.T) {
	store := testutil.NewTestStore(t)
	c := &clock{t: time.Now()}
	before := t.TempDir()
	first := startCertServer(t, store, before, c)
	id, machine := enrolled(t, first, store)

	// The changeover and retire phases pass while the machine is off; A is now renew-only.
	after := retireLayout(t, before, c.Now())
	second := startCertServer(t, store, after, c)

	m := second.connect(t, machine, nil)
	m.register(t)
	renewed := m.renew(t, id)
	leaf, err := x509.ParseCertificate(renewed.Certificate[0])
	require.NoError(t, err)
	require.NoError(t, leaf.CheckSignatureFrom(second.cm.CACert()), "the renewal comes from B")

	back := second.connect(t, renewed, nil)
	back.register(t)
	back.await(t, protocol.MsgTrustBundle)
}

func TestOfflineAcrossEmergencyUnvouchedRefused(t *testing.T) {
	store := testutil.NewTestStore(t)
	c := &clock{t: time.Now()}
	before := t.TempDir()
	first := startCertServer(t, store, before, c)
	c.Advance(time.Hour)
	unvouched := machinecert.Issue(t, first.issuer, uuid.New())

	after := emergencyLayout(t, before, c.Now())
	second := startCertServer(t, store, after, c, cert.WithCompromisedSince(c.Now().Add(-30*time.Minute)))

	m := second.connect(t, unvouched, nil)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), m.closedWith(t))
}

// retireLayout is the retire phase over the master key in old: a new issuer B, A renew-only, and
// B cross-signed by A.
func retireLayout(t *testing.T, old string, now time.Time) string {
	t.Helper()
	return nextIssuerLayout(t, old, now, cert.RenewOnlyFile)
}

// emergencyLayout is the emergency phase: B issues and A is vouched-only.
func emergencyLayout(t *testing.T, old string, now time.Time) string {
	t.Helper()
	return nextIssuerLayout(t, old, now, cert.VouchedOnlyFile)
}

func nextIssuerLayout(t *testing.T, old string, now time.Time, roleOfOld string) string {
	t.Helper()
	aPEM, err := os.ReadFile(filepath.Join(old, cert.IssuerCertFile))
	require.NoError(t, err)
	aKeyPEM, err := os.ReadFile(filepath.Join(old, cert.IssuerKeyFile))
	require.NoError(t, err)
	bPEM, bKeyPEM, err := cert.NewAuthority("Authority B", now)
	require.NoError(t, err)
	a, err := cert.ParseCertificatePEM(aPEM)
	require.NoError(t, err)
	b, err := cert.ParseCertificatePEM(bPEM)
	require.NoError(t, err)
	aKey, err := cert.ParseKeyPEM(aKeyPEM)
	require.NoError(t, err)
	cross, err := cert.CrossSign(b, a, aKey)
	require.NoError(t, err)
	dir := t.TempDir()
	for name, data := range map[string][]byte{
		cert.IssuerCertFile: bPEM, cert.IssuerKeyFile: bKeyPEM, roleOfOld: aPEM, cert.ChainFile: cross,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	return dir
}
