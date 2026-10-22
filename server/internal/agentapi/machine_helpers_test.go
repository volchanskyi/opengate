package agentapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

// deviceCert issues a certificate for id straight from the master key, for tests that stop at TLS
// or the handshake and never reach the ledger.
func deviceCert(tb testing.TB, cm *cert.Manager, id uuid.UUID) *tls.Certificate {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	require.NoError(tb, err)
	csr, err := cert.ParseDeviceRequest(der)
	require.NoError(tb, err)
	certDER, err := cm.IssueDevice(csr, id)
	require.NoError(tb, err)
	return &tls.Certificate{Certificate: [][]byte{certDER}, PrivateKey: key}
}

// selfSignedNamed is a certificate naming cn that no master key signed.
func selfSignedNamed(tb testing.TB, cn string) []byte {
	tb.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(tb, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(tb, err)
	return der
}

// clientTLS is the configuration a machine holding machine dials the listener with.
func clientTLS(cm *cert.Manager, machine *tls.Certificate) *tls.Config {
	return machinecert.ClientTLS(cm, machine)
}
