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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The certificate authority's private key must never leave the cluster.
//
// Signing agent certificates locally means copying that key onto a shared CI
// runner, where it is one misconfigured artifact upload away from being the
// credential that mints a trusted machine for the whole fleet. The public
// enrollment endpoint already does this job: the harness keeps its own private
// keys, sends only signing requests, and receives certificates the server
// signed.

func TestEnrollmentProducesACertificateWithoutHoldingTheAuthorityKey(t *testing.T) {
	server := enrollmentServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	issued, err := EnrollAgent(ctx, EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
		DeviceID:        "11111111-1111-1111-1111-111111111111",
	})
	require.NoError(t, err)

	require.NotNil(t, issued.Certificate.PrivateKey, "the harness keeps its own private key")
	require.NotEmpty(t, issued.Certificate.Certificate)
	require.NotEmpty(t, issued.CAPEM)

	leaf, err := x509.ParseCertificate(issued.Certificate.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", leaf.Subject.CommonName,
		"the issued certificate names the device that asked for it")
}

// The signing request carries a public key and nothing else. A harness that
// sent a private key anywhere would have defeated the point of asking.
func TestOnlyTheSigningRequestLeavesTheHarness(t *testing.T) {
	var body string
	server := enrollmentServerWithInspect(t, func(raw string) { body = raw })

	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         server.URL,
		EnrollmentToken: "enroll-token",
		DeviceID:        "22222222-2222-2222-2222-222222222222",
	})
	require.NoError(t, err)

	assert.Contains(t, body, "CERTIFICATE REQUEST")
	assert.NotContains(t, body, "PRIVATE KEY")
}

func TestEnrollmentRefusesADisallowedTarget(t *testing.T) {
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:         "http://opengate-server.opengate.svc.cluster.local:8080",
		EnrollmentToken: "enroll-token",
		DeviceID:        "33333333-3333-3333-3333-333333333333",
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
		DeviceID:        "44444444-4444-4444-4444-444444444444",
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
		DeviceID:        "55555555-5555-5555-5555-555555555555",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "certificate")
}

func TestEnrollmentRefusesAnEmptyToken(t *testing.T) {
	_, err := EnrollAgent(context.Background(), EnrollOptions{
		BaseURL:  "http://localhost:8080",
		DeviceID: "66666666-6666-6666-6666-666666666666",
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

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "loadtest-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)
	caCert, err := x509.ParseCertificate(caDER)
	require.NoError(t, err)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			CSRPEM string `json:"csr_pem"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		if inspect != nil {
			inspect(req.CSRPEM)
		}

		block, _ := pem.Decode([]byte(req.CSRPEM))
		require.NotNil(t, block)
		csr, err := x509.ParseCertificateRequest(block.Bytes)
		require.NoError(t, err)
		require.NoError(t, csr.CheckSignature())

		leaf := &x509.Certificate{
			SerialNumber: big.NewInt(2),
			Subject:      csr.Subject,
			NotBefore:    time.Now().Add(-time.Minute),
			NotAfter:     time.Now().Add(time.Hour),
			KeyUsage:     x509.KeyUsageDigitalSignature,
			ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		}
		leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, csr.PublicKey, caKey)
		require.NoError(t, err)

		_ = json.NewEncoder(w).Encode(map[string]string{
			"cert_pem":      string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})),
			"ca_pem":        string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})),
			"server_addr":   "opengate-staging-server:9090",
			"server_domain": "opengate-staging-server",
		})
	}))
	t.Cleanup(server.Close)
	return server
}
