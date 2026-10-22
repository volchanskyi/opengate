package agentapi

import (
	"context"
	"crypto/sha512"
	"crypto/tls"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/cert"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestSweepPacesOffers(t *testing.T) {
	env := newCertEnv(t, "")
	var machines []*tls.Certificate
	for i := 0; i < 5; i++ {
		_, machine := env.machine(t)
		machines = append(machines, machine)
	}
	var conns []*machineConn
	for _, machine := range machines {
		conns = append(conns, online(t, env, machine))
	}
	env.clock.Advance(61 * day)

	started := time.Now()
	offered := env.srv.SweepRenewals(context.Background())
	assert.Equal(t, len(machines), offered)
	assert.GreaterOrEqual(t, time.Since(started), time.Duration(len(machines)-1)*time.Second/offersPerSecond)
	var wg sync.WaitGroup
	for _, m := range conns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.await(t, protocol.MsgRenewCertificate, time.Second)
		}()
	}
	wg.Wait()
}

func TestFastPathAcceptsAnyTrustedHash(t *testing.T) {
	layout := changeoverLayout(t)
	env := newCertEnv(t, layout.dir)
	_, machine := env.machine(t)

	m := env.dialFast(t, machine, layout.oldHash)
	m.register(t)
	m.await(t, protocol.MsgTrustBundle, 5*time.Second)

	stale := env.dialFast(t, machine, [48]byte{7})
	stale.register(t)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseHandshakeRefused), stale.closedWith(t, 5*time.Second))
}

func TestChangeoverForcesRenewalOfOldIssuer(t *testing.T) {
	layout := changeoverLayout(t)
	env := newCertEnv(t, layout.dir)
	id, _ := env.machine(t)
	oldMachine := layout.issueFromOld(t, env, id, env.clock.Now())

	m := online(t, env, oldMachine)
	offer := m.await(t, protocol.MsgRenewCertificate, time.Second)
	assert.Equal(t, reasonAuthorityChange, offer.Reason)
}

func TestVouchedOnlyNeedsPreCompromiseLedgerEntry(t *testing.T) {
	layout := changeoverLayout(t)
	require.NoError(t, os.Remove(filepath.Join(layout.dir, cert.AcceptedFile)))
	require.NoError(t, os.WriteFile(filepath.Join(layout.dir, cert.VouchedOnlyFile), layout.oldPEM, 0o600))
	compromised := time.Now().Add(time.Hour)
	env := newCertEnv(t, layout.dir, cert.WithCompromisedSince(compromised))
	id, _ := env.machine(t)

	vouched := layout.issueFromOld(t, env, id, env.clock.Now())
	m := env.dial(t, vouched)
	m.register(t)
	m.await(t, protocol.MsgRenewCertificate, 5*time.Second)
	assert.Equal(t, 0, env.srv.ConnectedAgentCount(), "a vouched machine is admitted only to renew")

	unvouched := layout.issueFromOld(t, env, uuid.New(), compromised.Add(time.Minute))
	refused := env.dial(t, unvouched)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), refused.closedWith(t, 5*time.Second))
}

// changeover is a trust set mid-changeover: a new issuer, the old one accepted, and the new one
// cross-signed by the old.
type changeover struct {
	dir     string
	oldPEM  []byte
	oldKey  []byte
	oldHash [48]byte
}

func changeoverLayout(t *testing.T) changeover {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	oldCert, oldKey, err := cert.NewAuthority("Authority A", now.Add(-day))
	require.NoError(t, err)
	newCert, newKey, err := cert.NewAuthority("Authority B", now.Add(-time.Hour))
	require.NoError(t, err)
	a, err := cert.ParseCertificatePEM(oldCert)
	require.NoError(t, err)
	b, err := cert.ParseCertificatePEM(newCert)
	require.NoError(t, err)
	aKey, err := cert.ParseKeyPEM(oldKey)
	require.NoError(t, err)
	cross, err := cert.CrossSign(b, a, aKey)
	require.NoError(t, err)
	for name, data := range map[string][]byte{
		cert.IssuerCertFile: newCert, cert.IssuerKeyFile: newKey,
		cert.AcceptedFile: oldCert, cert.ChainFile: cross,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o600))
	}
	return changeover{dir: dir, oldPEM: oldCert, oldKey: oldKey, oldHash: sha512.Sum384(a.Raw)}
}

// issueFromOld signs a certificate for id with the old master key and records it as issued at.
func (c changeover) issueFromOld(t *testing.T, env *certEnv, id uuid.UUID, issuedAt time.Time) *tls.Certificate {
	t.Helper()
	scratch := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(scratch, cert.IssuerCertFile), c.oldPEM, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(scratch, cert.IssuerKeyFile), c.oldKey, 0o600))
	return enrolFrom(t, env, scratch, id, c.oldHash, issuedAt)
}
