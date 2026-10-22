package devicecert_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

// issuerOver is the issuer the product wires, over a fresh database and master key.
func issuerOver(t *testing.T) (*devicecert.Issuer, *db.PostgresStore) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	return machinecert.NewIssuer(t, store, cm, device.NewPostgresDevices(store.DB())), store
}

// requestNaming is a P-256 certificate request whose subject names cn.
func requestNaming(t *testing.T, cn string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	require.NoError(t, err)
	return der
}

func TestEnrollChoosesTheNumberAndRecordsTheCertificate(t *testing.T) {
	issuer, _ := issuerOver(t)
	asked := uuid.New()

	id, der, err := issuer.Enroll(context.Background(), requestNaming(t, asked.String()))
	require.NoError(t, err)

	assert.NotEqual(t, asked, id, "the request's own subject is ignored")
	leaf, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	assert.Equal(t, id.String(), leaf.Subject.CommonName)
	entry, recorded := issuer.Ledger().Lookup(certledger.FingerprintOf(der))
	require.True(t, recorded, "the certificate is recorded before it is handed out")
	assert.Equal(t, id, entry.DeviceID)
	assert.NotEmpty(t, issuer.CACertPEM())
}

func TestEnrollRefusesARequestThatIsNotAP256Request(t *testing.T) {
	issuer, _ := issuerOver(t)

	_, _, err := issuer.Enroll(context.Background(), []byte("not a request"))
	assert.ErrorIs(t, err, devicecert.ErrInvalidRequest)
}

func TestEnrollStopsWhenTheDeviceStoreCannotAnswer(t *testing.T) {
	issuer, _ := issuerOver(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, der, err := issuer.Enroll(ctx, requestNaming(t, ""))
	require.Error(t, err)
	assert.ErrorContains(t, err, "check device number")
	assert.Nil(t, der)
}

func TestIssueHandsOutNoCertificateWhenTheRecordFails(t *testing.T) {
	issuer, store := issuerOver(t)
	csr, err := cert.ParseDeviceRequest(requestNaming(t, ""))
	require.NoError(t, err)
	_, err = store.DB().Exec(`ALTER TABLE device_certificates RENAME TO device_certificates_away`)
	require.NoError(t, err)

	der, err := issuer.Issue(context.Background(), csr, uuid.New(), certledger.SourceRenew)
	require.Error(t, err)
	assert.Nil(t, der)
}
