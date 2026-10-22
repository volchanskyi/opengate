package certledger

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log/slog"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

var (
	issuerA = [48]byte{0xa}
	issuerB = [48]byte{0xb}
	t0      = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
)

func newLedger(t *testing.T) *Store {
	t.Helper()
	return New(testutil.NewTestStore(t).DB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// leaf is a certificate for device starting at notBefore; the ledger reads no signature.
func leaf(t *testing.T, device uuid.UUID, notBefore time.Time) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: device.String()},
		NotBefore:    notBefore,
		NotAfter:     notBefore.Add(90 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	c, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return c
}

func record(t *testing.T, s *Store, c *x509.Certificate, issuer [48]byte, source Source) Fingerprint {
	t.Helper()
	e, err := s.Record(context.Background(), c, issuer, source, c.NotBefore)
	require.NoError(t, err)
	return e.Fingerprint
}

func rowCount(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	require.NoError(t, s.db.QueryRow(`SELECT count(*) FROM device_certificates`).Scan(&n))
	return n
}

func TestRecordIdempotentOnFingerprint(t *testing.T) {
	s := newLedger(t)
	c := leaf(t, uuid.New(), t0)
	first := record(t, s, c, issuerA, SourceEnroll)
	second := record(t, s, c, issuerA, SourceEnroll)
	assert.Equal(t, first, second)
	assert.Equal(t, 1, rowCount(t, s))
}

func TestInstalledCertSupersedesOlderAtOnce(t *testing.T) {
	s := newLedger(t)
	device := uuid.New()
	old := record(t, s, leaf(t, device, t0), issuerA, SourceEnroll)
	s.MarkSeen(old, t0)
	renewed := record(t, s, leaf(t, device, t0.Add(60*24*time.Hour)), issuerA, SourceRenew)

	superseded, err := s.MarkInstalled(context.Background(), renewed, t0.Add(60*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, []Fingerprint{old}, superseded)
	assert.True(t, s.Superseded(old))
	assert.False(t, s.Superseded(renewed))
}

func TestLostInstalledNoticeSupersedesOnFirstPresentation(t *testing.T) {
	s := newLedger(t)
	device := uuid.New()
	old := record(t, s, leaf(t, device, t0), issuerA, SourceEnroll)
	s.MarkSeen(old, t0)
	renewed := record(t, s, leaf(t, device, t0.Add(time.Hour)), issuerA, SourceRenew)
	require.False(t, s.Superseded(old), "an uninstalled renewal supersedes nothing yet")

	assert.Equal(t, []Fingerprint{old}, s.MarkSeen(renewed, t0.Add(2*time.Hour)))
	assert.True(t, s.Superseded(old))
}

func TestUnseenNewerCertDoesNotSupersede(t *testing.T) {
	s := newLedger(t)
	device := uuid.New()
	old := record(t, s, leaf(t, device, t0), issuerA, SourceEnroll)
	s.MarkSeen(old, t0)
	for i := 1; i <= 3; i++ {
		record(t, s, leaf(t, device, t0.Add(time.Duration(i)*time.Hour)), issuerA, SourceRenew)
	}
	assert.False(t, s.Superseded(old), "lost replies leave the machine's certificate current")
}

func TestIndexWarmedAtStartAndUpdatedOnIssue(t *testing.T) {
	store := testutil.NewTestStore(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	first := New(store.DB(), logger)
	device := uuid.New()
	old := record(t, first, leaf(t, device, t0), issuerA, SourceEnroll)
	_, err := first.MarkInstalled(context.Background(), old, t0)
	require.NoError(t, err)

	restarted := New(store.DB(), logger)
	_, ok := restarted.Lookup(old)
	require.False(t, ok, "nothing is known before warming")
	require.NoError(t, restarted.Warm(context.Background()))
	e, ok := restarted.Lookup(old)
	require.True(t, ok)
	assert.True(t, e.Seen, "the sighting survives the restart")

	issued := record(t, restarted, leaf(t, device, t0.Add(time.Hour)), issuerA, SourceRenew)
	_, ok = restarted.Lookup(issued)
	assert.True(t, ok, "an issuance is in the index without a reload")
	assert.True(t, restarted.HasDevice(device))
}

func TestSlowLedgerWriteNeverDelaysAdmission(t *testing.T) {
	s := newLedger(t)
	fp := record(t, s, leaf(t, uuid.New(), t0), issuerA, SourceEnroll)

	// A transaction holding the row stands in for a database that is slow to answer.
	tx, err := s.db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	_, err = tx.Exec(`SELECT 1 FROM device_certificates WHERE fingerprint = $1 FOR UPDATE`, fp[:])
	require.NoError(t, err)

	started := time.Now()
	s.MarkSeen(fp, t0)
	assert.Less(t, time.Since(started), 50*time.Millisecond)
	e, _ := s.Lookup(fp)
	assert.True(t, e.Seen, "admission reads the sighting from memory at once")

	require.NoError(t, tx.Rollback())
	require.Eventually(t, func() bool {
		var seen bool
		_ = s.db.QueryRow(`SELECT first_seen_at IS NOT NULL FROM device_certificates WHERE fingerprint = $1`,
			fp[:]).Scan(&seen)
		return seen
	}, 5*time.Second, 20*time.Millisecond, "the sighting reaches the table once the row is free")
}
