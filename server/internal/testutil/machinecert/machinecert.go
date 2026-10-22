// Package machinecert gives tests machine credentials the way the product issues them: through
// the device certificate issuer, recorded in the ledger admission reads.
package machinecert

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/devicecert"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
)

// NewIssuer builds the issuer the product wires, over the store's own schema.
func NewIssuer(tb testing.TB, store *db.PostgresStore, cm *cert.Manager, devices device.Repository) *devicecert.Issuer {
	tb.Helper()
	ledger := certledger.New(store.DB(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	return devicecert.NewIssuer(cm, ledger, devices, lifecycle.NewTombstoneStore(store.DB()))
}

// Issue records a certificate for deviceID under a fresh P-256 key, as a renewal does.
func Issue(tb testing.TB, issuer *devicecert.Issuer, deviceID uuid.UUID) *tls.Certificate {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("generate machine key: %v", err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		tb.Fatalf("build machine request: %v", err)
	}
	csr, err := cert.ParseDeviceRequest(der)
	if err != nil {
		tb.Fatalf("parse machine request: %v", err)
	}
	certDER, err := issuer.Issue(context.Background(), csr, deviceID, certledger.SourceEnroll)
	if err != nil {
		tb.Fatalf("issue machine certificate: %v", err)
	}
	return &tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}
}

// ClientTLS is the configuration a machine dials the agent listener with: its certificate, and
// the trust bundle to verify the server against.
func ClientTLS(cm *cert.Manager, machine *tls.Certificate) *tls.Config {
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(cm.CACertPEM())
	return &tls.Config{
		ServerName:   "localhost",
		RootCAs:      pool,
		Certificates: []tls.Certificate{*machine},
		MinVersion:   tls.VersionTLS13,
		NextProtos:   []string{"opengate"},
	}
}
