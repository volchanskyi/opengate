package agentapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log/slog"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// online connects and registers the machine, returning once the server acknowledged it.
func online(t *testing.T, env *certEnv, machine *tls.Certificate) *machineConn {
	t.Helper()
	m := env.dial(t, machine)
	m.register(t)
	m.await(t, protocol.MsgTrustBundle, 5*time.Second)
	return m
}

// renew answers an offer with a request from a fresh key and returns the reply's type.
func renew(t *testing.T, m *machineConn, id uuid.UUID) *protocol.ControlMessage {
	t.Helper()
	csr, _ := renewalRequest(t, id.String())
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateSigningRequest, CSR: csr})
	return awaitEither(t, m, protocol.MsgCertificateIssued, protocol.MsgCertificateRefused)
}

func awaitEither(t *testing.T, m *machineConn, types ...protocol.ControlMessageType) *protocol.ControlMessage {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg, ok := <-m.inbox:
			require.True(t, ok, "the connection closed")
			for _, want := range types {
				if msg.Type == want {
					return msg
				}
			}
		case <-deadline:
			t.Fatalf("none of %v arrived", types)
		}
	}
}

func noMessage(t *testing.T, m *machineConn, unwanted protocol.ControlMessageType, wait time.Duration) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case msg, ok := <-m.inbox:
			if !ok {
				return
			}
			require.NotEqual(t, unwanted, msg.Type)
		case <-deadline:
			return
		}
	}
}

func TestRenewalOfferedAtTwoThirdsOfLifetime(t *testing.T) {
	env := newCertEnv(t, "")
	_, machine := env.machine(t)

	env.clock.Advance(59 * day)
	noMessage(t, online(t, env, machine), protocol.MsgRenewCertificate, 500*time.Millisecond)

	env.clock.Advance(day + time.Hour)
	m := online(t, env, machine)
	offer := m.await(t, protocol.MsgRenewCertificate, time.Second)
	assert.Equal(t, reasonDue, offer.Reason)
}

func TestRenewalDueFromTheCertificatesOwnDates(t *testing.T) {
	// The master key was re-signed for a longer life under the same key, so a leaf its earlier
	// expiry cut to 30 days is due at day 20 of its own life, not at day 60 of a full one.
	now := time.Now()
	key := newAuthorityKey(t)
	short := t.TempDir()
	writeIssuerWithKey(t, short, key, now.Add(-day), now.Add(30*day))
	long := t.TempDir()
	writeIssuerWithKey(t, long, key, now.Add(-day), now.Add(3650*day))
	env := newCertEnv(t, long)
	id, _ := env.machine(t)
	machine := enrolFrom(t, env, short, id, env.cm.IssuerHash(), env.clock.Now())

	env.clock.Advance(19 * day)
	noMessage(t, online(t, env, machine), protocol.MsgRenewCertificate, 500*time.Millisecond)
	env.clock.Advance(2 * day)
	offer := online(t, env, machine).await(t, protocol.MsgRenewCertificate, time.Second)
	assert.Equal(t, reasonDue, offer.Reason)
}

func TestNoOfferWhenTheAuthorityCapsTheNewCertificate(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeIssuerWithKey(t, dir, newAuthorityKey(t), now.Add(-day), now.Add(30*day))
	env := newCertEnv(t, dir)
	_, machine := env.machine(t)

	// Due at day 20 of its 30, but a renewal would end on the same day the master key does.
	env.clock.Advance(25 * day)
	noMessage(t, online(t, env, machine), protocol.MsgRenewCertificate, 500*time.Millisecond)
}

func TestCensusWarnsBeforeTheIssuerExpires(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeIssuerWithKey(t, dir, newAuthorityKey(t), now.Add(-day), now.Add(30*day))
	logs := &logBuffer{}
	env := newCertEnvLogging(t, dir, slog.New(slog.NewTextHandler(logs, nil)))

	env.srv.LogAuthorityCensus()
	assert.Contains(t, logs.String(), "master key expires in 29 days, start the changeover")
	assert.Contains(t, logs.String(), "authority census")
}

// enrolFrom issues a certificate for id from the master key in dir and records it under issuerHash.
func enrolFrom(t *testing.T, env *certEnv, dir string, id uuid.UUID, issuerHash [48]byte, at time.Time) *tls.Certificate {
	t.Helper()
	m, err := cert.NewManager(dir, cert.WithClock(env.clock.Now))
	require.NoError(t, err)
	machine := deviceCert(t, m, id)
	leaf, err := x509.ParseCertificate(machine.Certificate[0])
	require.NoError(t, err)
	_, err = env.issuer.Ledger().Record(context.Background(), leaf, issuerHash, certledger.SourceEnroll, at)
	require.NoError(t, err)
	return machine
}

func newAuthorityKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

// writeIssuerWithKey writes an issuing master key valid between notBefore and notAfter into dir.
func writeIssuerWithKey(t *testing.T, dir string, key *ecdsa.PrivateKey, notBefore, notAfter time.Time) {
	t.Helper()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(notAfter.Unix()),
		Subject:               pkix.Name{CommonName: "OpenGate CA"},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, cert.IssuerCertFile),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, cert.IssuerKeyFile),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))
}

func fingerprintBytes(machine *tls.Certificate) []byte {
	fp := certledger.FingerprintOf(machine.Certificate[0])
	return fp[:]
}

// requestForKey builds a renewal request reusing the machine's current key.
func requestForKey(t *testing.T, id uuid.UUID, machine *tls.Certificate) []byte {
	t.Helper()
	tmpl := &x509.CertificateRequest{}
	tmpl.Subject.CommonName = id.String()
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, machine.PrivateKey)
	require.NoError(t, err)
	return der
}
