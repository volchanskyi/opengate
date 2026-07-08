package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
	"log/slog"
	"os"
	"testing"
)

// generateTestCSRPEM creates a valid PEM-encoded CERTIFICATE REQUEST for testing.
func generateTestCSRPEM() string {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "test-agent"},
	}, key)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
}

// testCSRPEM is a valid PEM-encoded CERTIFICATE REQUEST for testing.
var testCSRPEM = generateTestCSRPEM()

// stubCertProvider is a test double for CertProvider.
type stubCertProvider struct {
	pem    []byte
	signFn func(csrDER []byte) ([]byte, error)
}

func (s *stubCertProvider) CACertPEM() []byte { return s.pem }

func (s *stubCertProvider) SignAgentCSR(csrDER []byte) ([]byte, error) {
	if s.signFn != nil {
		return s.signFn(csrDER)
	}
	return nil, fmt.Errorf("SignAgentCSR not configured")
}

func newTestServerWithCert(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	store := testutil.NewTestStore(t)
	cfg := testJWTConfig()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := NewServer(ServerConfig{
		Store:          store,
		Audit:          testutil.NewTestAudit(t, store),
		DeviceUpdates:  testutil.NewTestDeviceUpdates(t, store),
		Enrollment:     testutil.NewTestEnrollment(t, store),
		SecurityGroups: testutil.NewTestSecurityGroups(t, store),
		Devices:        testutil.NewTestDevices(t, store),
		Groups:         testutil.NewTestGroups(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
		AMTDevices:     testutil.NewTestAMTDevices(t, store),
		Sessions:       testutil.NewTestSessions(t, store),
		Users:          testutil.NewTestUsers(t, store),
		JWT:            cfg,
		Agents:         &stubAgentGetter{},
		AMT:            &stubAMTOperator{},
		Cert:           &stubCertProvider{pem: []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n")},
		Relay:          relay.NewRelay(slog.Default()),
		Notifier:       &notifications.NoopNotifier{},
		Logger:         logger,
	})
	return srv, cfg
}

func newTestServerWithSigning(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	srv, cfg := newTestServerWithCert(t)
	srv.cert = &stubCertProvider{
		pem: []byte("-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n"),
		signFn: func(_ []byte) ([]byte, error) {
			return []byte("fake-signed-cert"), nil
		},
	}
	return srv, cfg
}
