package agentapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

func TestUnsolicitedCSRRefused(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	m := online(t, env, machine)

	reply := renew(t, m, id)
	assert.Equal(t, protocol.MsgCertificateRefused, reply.Type)
	assert.Equal(t, refusedUnsolicited, reply.Reason)
}

func TestCSRForAnotherDeviceRefused(t *testing.T) {
	o := dueAndOffered(t, nil)

	reply := renew(t, o.conn, uuid.New())
	assert.Equal(t, refusedWrongDevice, reply.Reason)
}

func TestKeyReuseRefused(t *testing.T) {
	o := dueAndOffered(t, nil)

	csr := requestForKey(t, o.id, o.machine)
	o.conn.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateSigningRequest, CSR: csr})
	reply := awaitEither(t, o.conn, protocol.MsgCertificateRefused, protocol.MsgCertificateIssued)
	assert.Equal(t, refusedKeyReuse, reply.Reason)
}

func TestTombstonedOrUnknownDeviceRefused(t *testing.T) {
	o := dueAndOffered(t, nil)
	o.env.srv.tombstones.Store(o.id, struct{}{})

	assert.Equal(t, refusedDeleted, renew(t, o.conn, o.id).Reason)
}

func TestIssuedCarriesTrustBundle(t *testing.T) {
	o := dueAndOffered(t, nil)

	reply := renew(t, o.conn, o.id)
	require.Equal(t, protocol.MsgCertificateIssued, reply.Type)
	assert.Equal(t, string(o.env.cm.CACertPEM()), reply.CABundle)
	leaf, err := x509.ParseCertificate(reply.Cert)
	require.NoError(t, err)
	assert.Equal(t, o.id.String(), leaf.Subject.CommonName)
	_, recorded := o.env.issuer.Ledger().Lookup(certledger.FingerprintOf(reply.Cert))
	assert.True(t, recorded, "the renewal was recorded before it was sent")
}

func TestCertWithheldWhenLedgerFails(t *testing.T) {
	o := dueAndOffered(t, nil)
	_, err := o.env.store.DB().Exec(`ALTER TABLE device_certificates RENAME TO device_certificates_away`)
	require.NoError(t, err)

	reply := renew(t, o.conn, o.id)
	assert.Equal(t, protocol.MsgCertificateRefused, reply.Type)
	assert.Equal(t, refusedServerError, reply.Reason)
	assert.Empty(t, reply.Cert)
}

func TestFourthRenewalIn24hRefusedWithRetryAfter(t *testing.T) {
	o := dueAndOffered(t, func(env *certEnv, id uuid.UUID) { orphanRenewals(t, env, id, time.Hour) })

	reply := renew(t, o.conn, o.id)
	assert.Equal(t, refusedRateLimited, reply.Reason)
	assert.Greater(t, reply.RetryAfter, uint32(0))
	assert.LessOrEqual(t, reply.RetryAfter, uint32(86400))

	o.env.clock.Advance(22 * time.Hour)
	again := online(t, o.env, o.machine)
	again.await(t, protocol.MsgRenewCertificate, time.Second)
	assert.Equal(t, protocol.MsgCertificateIssued, renew(t, again, o.id).Type, "a lost reply delays a renewal, never blocks it")
}

func TestRateLimitSurvivesRestart(t *testing.T) {
	env := newCertEnv(t, "")
	id, _ := env.machine(t)
	orphanRenewals(t, env, id, 0)
	restarted := certledger.New(env.store.DB(), testLogger())
	require.NoError(t, restarted.Warm(context.Background()))
	n, err := restarted.RenewalsSince(context.Background(), id, env.clock.Now().Add(-renewalWindow))
	require.NoError(t, err)
	assert.Equal(t, renewalsPerDay, n)
}

func TestConcurrentCSRsSerialized(t *testing.T) {
	o := dueAndOffered(t, func(env *certEnv, id uuid.UUID) { env.srv.renewals.inFlight.Store(id, struct{}{}) })

	assert.Equal(t, refusedInFlight, renew(t, o.conn, o.id).Reason)
}

// offered is a machine due for renewal, online, that has received the server's offer.
type offered struct {
	env     *certEnv
	id      uuid.UUID
	machine *tls.Certificate
	conn    *machineConn
}

// dueAndOffered brings a machine two thirds into its certificate's life online and waits for the
// offer; before, when set, runs once the machine is due and before it connects.
func dueAndOffered(t *testing.T, before func(env *certEnv, id uuid.UUID)) offered {
	t.Helper()
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(61 * day)
	if before != nil {
		before(env, id)
	}
	conn := online(t, env, machine)
	conn.await(t, protocol.MsgRenewCertificate, time.Second)
	return offered{env: env, id: id, machine: machine, conn: conn}
}

// orphanRenewals records a day's renewals the machine never received, gap apart; they still count.
func orphanRenewals(t *testing.T, env *certEnv, id uuid.UUID, gap time.Duration) {
	t.Helper()
	for i := 0; i < renewalsPerDay; i++ {
		env.clock.Advance(gap)
		orphan := machinecert.Issue(t, env.issuer, id)
		_, err := env.store.DB().Exec(`UPDATE device_certificates SET source = 'renew' WHERE fingerprint = $1`,
			fingerprintBytes(orphan))
		require.NoError(t, err)
	}
}
