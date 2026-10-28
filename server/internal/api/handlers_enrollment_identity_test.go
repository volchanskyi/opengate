package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// csrNaming builds a P-256 request whose subject names cn, or nothing when cn is empty.
func csrNaming(t *testing.T, cn string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: cn},
	}, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

// enrolWith posts a request with a fresh unlimited token and returns the decoded reply and the
// certificate it carried.
func enrolWith(t *testing.T, srv *Server, adminToken, csrPEM string) (EnrollResponse, *x509.Certificate) {
	t.Helper()
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{})
	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: csrPEM})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp EnrollResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.CertPem)
	block, _ := pem.Decode([]byte(*resp.CertPem))
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return resp, leaf
}

func TestEnrollIgnoresTheRequestedSubject(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	existing := uuid.New()
	seedEnrolledDevice(t, srv, existing)

	for name, cn := range map[string]string{
		"an existing device's number": existing.String(),
		"a non-UUID name":             "workstation-7",
		"nothing":                     "",
	} {
		resp, leaf := enrolWith(t, srv, adminToken, csrNaming(t, cn))
		assigned, err := uuid.Parse(leaf.Subject.CommonName)
		require.NoError(t, err, name)
		assert.NotEqual(t, existing, assigned, "%s: the request claimed a number", name)
		require.NotNil(t, resp.DeviceId)
		assert.Equal(t, assigned, *resp.DeviceId, name)
	}
}

func TestEnrollReplyCarriesTheNumberInTheCertificate(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

	resp, leaf := enrolWith(t, srv, adminToken, csrNaming(t, ""))
	require.NotNil(t, resp.DeviceId)
	assert.Equal(t, resp.DeviceId.String(), leaf.Subject.CommonName)
}

func TestEnrollRecordsIssuedCertificate(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

	resp, leaf := enrolWith(t, srv, adminToken, csrNaming(t, ""))
	fingerprint := certledger.FingerprintOf(leaf.Raw)
	var deviceID uuid.UUID
	var source string
	require.NoError(t, srv.store.DB().QueryRow(
		`SELECT device_id, source FROM device_certificates WHERE fingerprint = $1`,
		fingerprint[:]).Scan(&deviceID, &source))
	assert.Equal(t, *resp.DeviceId, deviceID)
	assert.Equal(t, string(certledger.SourceEnroll), source)
}

func TestEnrollAssignsANumberWithNoDeviceDeletedOrLedgerRow(t *testing.T) {
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)

	withDevice, deleted, onLedger := uuid.New(), uuid.New(), uuid.New()
	seedEnrolledDevice(t, srv, withDevice)
	require.NoError(t, lifecycle.NewTombstoneStore(srv.store.DB()).
		TombstoneDevice(testTenantContext(t), dbtx.DefaultTenantID, deleted, nil))
	issuer := srv.cert.(interface{ Ledger() *certledger.Store }).Ledger()
	_, ledgerLeaf := enrolWith(t, srv, adminToken, csrNaming(t, ""))
	onLedger = uuid.MustParse(ledgerLeaf.Subject.CommonName)
	require.True(t, issuer.HasDevice(onLedger))

	// The number source hands out every taken number first, then a free one.
	free := uuid.New()
	var sequence bytes.Buffer
	for _, id := range []uuid.UUID{withDevice, deleted, onLedger, free} {
		sequence.Write(id[:])
	}
	uuid.SetRand(io.MultiReader(&sequence, rand.Reader))
	t.Cleanup(func() { uuid.SetRand(nil) })

	resp, _ := enrolWith(t, srv, adminToken, csrNaming(t, ""))
	assert.Equal(t, free, *resp.DeviceId)
}

func TestEnrollReturns500WhenLedgerWriteFails(t *testing.T) {
	t.Parallel()
	srv, cfg := newTestServerWithSigning(t)
	_, adminToken := seedTestUser(t, srv, cfg, "admin@test.com", true)
	tok := createEnrollmentTokenViaAPI(t, srv, adminToken, CreateEnrollmentTokenRequest{})
	_, err := srv.store.DB().Exec(`ALTER TABLE device_certificates RENAME TO device_certificates_away`)
	require.NoError(t, err)

	w := doRequest(srv, http.MethodPost, "/api/v1/enroll/"+tok.Token, "", EnrollRequest{CsrPem: csrNaming(t, "")})
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.NotContains(t, w.Body.String(), "BEGIN CERTIFICATE", "an unrecorded certificate was sent")
}

// seedEnrolledDevice files a device row under id in the test tenant.
func seedEnrolledDevice(t *testing.T, srv *Server, id uuid.UUID) {
	t.Helper()
	site := testutil.SeedSite(t, testTenantContext(t), srv.store)
	require.NoError(t, srv.devices.Upsert(testTenantContext(t), &device.Device{
		ID: id, SiteID: site.ID, Hostname: "existing", OS: "linux", Status: device.StatusOffline,
	}))
}
