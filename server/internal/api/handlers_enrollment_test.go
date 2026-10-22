package api

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/auth"
	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	"github.com/volchanskyi/opengate/server/internal/notifications"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/testutil"
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

// stubCertProvider answers the probe with a bundle and refuses every request to be signed.
type stubCertProvider struct {
	pem []byte
}

func (s *stubCertProvider) CACertPEM() []byte { return s.pem }

func (s *stubCertProvider) Enroll(context.Context, []byte) (uuid.UUID, []byte, error) {
	return uuid.Nil, nil, fmt.Errorf("%w: this server signs nothing", devicecert.ErrInvalidRequest)
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
		Sites:          testutil.NewTestSites(t, store),
		Hardware:       testutil.NewTestHardware(t, store),
		WebPush:        testutil.NewTestWebPush(t, store),
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

// newTestServerWithSigning enrols through the issuer the product uses: a real master key and a
// real ledger, in the test's own database schema.
func newTestServerWithSigning(t *testing.T) (*Server, *auth.JWTConfig) {
	t.Helper()
	srv, cfg := newTestServerWithCert(t)
	srv.cert = newTestIssuer(t, srv)
	return srv, cfg
}

func newTestIssuer(t *testing.T, srv *Server) *devicecert.Issuer {
	t.Helper()
	cm, err := cert.NewManager(t.TempDir())
	require.NoError(t, err)
	db := srv.store.DB()
	ledger := certledger.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return devicecert.NewIssuer(cm, ledger, srv.devices, lifecycle.NewTombstoneStore(db))
}
