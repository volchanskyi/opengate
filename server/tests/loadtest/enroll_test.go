package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrollmentProducesACertificateWithoutHoldingTheAuthorityKey(t *testing.T) {
	server := enrollmentServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	issued, err := EnrollAgent(ctx, EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
	})
	require.NoError(t, err)

	require.NotNil(t, issued.Certificate.PrivateKey, "the harness keeps its own private key")
	require.NotEmpty(t, issued.Certificate.Certificate)
	require.NotEmpty(t, issued.CAPEM)

	leaf, err := x509.ParseCertificate(issued.Certificate.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, issued.DeviceID, leaf.Subject.CommonName,
		"the issued certificate carries the number the server chose")
}

func TestOnlyTheSigningRequestLeavesTheHarness(t *testing.T) {
	var body string
	server := enrollmentServerWithInspect(t, func(raw string) { body = raw })

	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
	})
	require.NoError(t, err)

	assert.Contains(t, body, "CERTIFICATE REQUEST")
	assert.NotContains(t, body, "PRIVATE KEY")
}

func TestEnrollmentRefusesADisallowedTarget(t *testing.T) {
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         "http://opengate-server.opengate.svc.cluster.local:8080",
		EnrollmentToken: "enroll-token",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

func TestEnrollmentNamesAServerThatRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"token exhausted"}`))
	}))
	t.Cleanup(server.Close)

	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "spent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "410")
}

func TestEnrollmentRefusesAResponseWithNoCertificate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"ca_pem": "", "server_addr": "x", "server_domain": "y"})
	}))
	t.Cleanup(server.Close)

	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate")
}

func TestEnrollmentNamesAFreshDeviceInEachRequest(t *testing.T) {
	var asked []string
	server := enrollmentServerWithInspect(t, func(raw string) {
		asked = append(asked, parseRequest(t, raw).Subject.CommonName)
	})

	for range 2 {
		_, err := EnrollAgent(context.Background(), EnrollOptions{BaseURL: server.URL, EnrollmentToken: "enroll-token"})
		require.NoError(t, err)
	}

	require.Len(t, asked, 2)
	for _, name := range asked {
		_, err := uuid.Parse(name)
		require.NoError(t, err, "the request names a device number, so a server that copies it issues one")
	}
	assert.NotEqual(t, asked[0], asked[1])
}

func TestEnrollmentTakesTheCertificatesDeviceWhenTheReplyNamesNone(t *testing.T) {
	var asked string
	server := copyingEnrollmentServer(t, func(name string) string { asked = name; return name })

	issued, err := EnrollAgent(context.Background(), EnrollOptions{BaseURL: server.URL, EnrollmentToken: "enroll-token"})
	require.NoError(t, err)

	require.NotEmpty(t, asked)
	assert.Equal(t, asked, issued.DeviceID)
	assert.Equal(t, asked, issued.Certificate.Leaf.Subject.CommonName)
}

func TestEnrollmentRefusesACertificateForAnotherDeviceThanAsked(t *testing.T) {
	server := copyingEnrollmentServer(t, func(string) string { return uuid.NewString() })

	_, err := EnrollAgent(context.Background(), EnrollOptions{BaseURL: server.URL, EnrollmentToken: "enroll-token"})
	require.ErrorIs(t, err, ErrEnrollmentFailed)
}

func TestEnrollmentRefusesAReplyNamingADeviceItsCertificateDoesNotCarry(t *testing.T) {
	authority := newFakeAuthority(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		answer := authority.sign(t, r, nil, func(string) string { return uuid.NewString() })
		answer["device_id"] = uuid.NewString()
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)

	_, err := EnrollAgent(context.Background(), EnrollOptions{BaseURL: server.URL, EnrollmentToken: "enroll-token"})
	require.ErrorIs(t, err, ErrEnrollmentFailed)
}

func TestEnrollmentRefusesAnEmptyToken(t *testing.T) {
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL: "http://localhost:8080",
	})
	require.Error(t, err)
}

// enrollmentServer stands up an endpoint shaped like the real one: it signs
// what it is sent with a throwaway authority and returns the certificate.
func enrollmentServer(t *testing.T) *httptest.Server {
	t.Helper()
	return enrollmentServerWithInspect(t, nil)
}

func enrollmentServerWithInspect(t *testing.T, inspect func(string)) *httptest.Server {
	t.Helper()
	authority := newFakeAuthority(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var deviceID string
		answer := authority.sign(t, r, inspect, func(string) string {
			// The server chooses the number, as the product does, whatever the request names.
			deviceID = uuid.NewString()
			return deviceID
		})
		answer["device_id"] = deviceID
		_ = json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(server.Close)
	return server
}

// copyingEnrollmentServer signs the way a server built before the certificate record does: the
// certificate carries the name issuedName returns for the request's, and the reply names no device.
func copyingEnrollmentServer(t *testing.T, issuedName func(asked string) string) *httptest.Server {
	t.Helper()
	authority := newFakeAuthority(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(authority.sign(t, r, nil, issuedName))
	}))
	t.Cleanup(server.Close)
	return server
}

// fakeAuthority is a throwaway certificate authority that signs enrollment requests.
type fakeAuthority struct {
	key  *ecdsa.PrivateKey
	cert *x509.Certificate
	der  []byte
}

func newFakeAuthority(t *testing.T) *fakeAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "loadtest-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return &fakeAuthority{key: key, cert: cert, der: der}
}

// sign answers one enrollment request, handed to inspect when set, with a certificate naming
// issuedName's answer for the request's own name, in the reply's shape minus its device number.
func (a *fakeAuthority) sign(
	t *testing.T, r *http.Request, inspect func(string), issuedName func(asked string) string,
) map[string]string {
	t.Helper()
	var req struct {
		CSRPEM string `json:"csr_pem"`
	}
	require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
	if inspect != nil {
		inspect(req.CSRPEM)
	}
	csr := parseRequest(t, req.CSRPEM)

	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: issuedName(csr.Subject.CommonName)},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, a.cert, csr.PublicKey, a.key)
	require.NoError(t, err)
	return map[string]string{
		"cert_pem":      string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})),
		"ca_pem":        string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.der})),
		"server_addr":   "opengate-staging-server:9090",
		"server_domain": "opengate-staging-server",
	}
}

func parseRequest(t *testing.T, csrPEM string) *x509.CertificateRequest {
	t.Helper()
	block, _ := pem.Decode([]byte(csrPEM))
	require.NotNil(t, block)
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	require.NoError(t, err)
	require.NoError(t, csr.CheckSignature())
	return csr
}
