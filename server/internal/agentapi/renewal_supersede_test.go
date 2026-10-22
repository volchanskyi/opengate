package agentapi

import (
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestAMachineRenewsOverItsLiveConnection(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(61 * day)
	m := online(t, env, machine)
	m.await(t, protocol.MsgRenewCertificate, time.Second)

	started := time.Now()
	reply := renew(t, m, id)
	require.Equal(t, protocol.MsgCertificateIssued, reply.Type)
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})
	require.Eventually(t, func() bool {
		return env.issuer.Ledger().Superseded(certledger.FingerprintOf(machine.Certificate[0]))
	}, 2*time.Second, 10*time.Millisecond)
	assert.Less(t, time.Since(started), 2*time.Second)
	noMessage(t, m, protocol.MsgAgentDeregistered, 300*time.Millisecond)
	assert.Equal(t, 1, env.srv.ConnectedAgentCount(), "the renewing connection stays up")
}

func TestRenewalRefusedOnASupersededCertificate(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(61 * day)
	m := online(t, env, machine)
	m.await(t, protocol.MsgRenewCertificate, time.Second)
	machineReissue(t, env, id)

	assert.Equal(t, refusedSuperseded, renew(t, m, id).Reason)
}

func TestSupersessionClosesLiveConnectionsOnTheOldCertificate(t *testing.T) {
	// Two copies of one expired identity are each admitted only to renew; the first to install
	// its renewal keeps the device, and the other is closed.
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(leafLife + day)
	copyOfDisk := env.dial(t, machine)
	copyOfDisk.register(t)
	copyOfDisk.await(t, protocol.MsgRenewCertificate, 5*time.Second)
	original := env.dial(t, machine)
	original.register(t)
	original.await(t, protocol.MsgRenewCertificate, 5*time.Second)

	require.Equal(t, protocol.MsgCertificateIssued, renew(t, original, id).Type)
	original.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})

	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), copyOfDisk.closedWith(t, 5*time.Second))
}

func TestStolenOldKeyRefusedOnceTheRealMachineInstalls(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(61 * day)
	m := online(t, env, machine)
	m.await(t, protocol.MsgRenewCertificate, time.Second)
	require.Equal(t, protocol.MsgCertificateIssued, renew(t, m, id).Type)
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})
	require.Eventually(t, func() bool {
		return env.issuer.Ledger().Superseded(certledger.FingerprintOf(machine.Certificate[0]))
	}, 2*time.Second, 10*time.Millisecond)

	copyOfOldKey := env.dial(t, machine)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), copyOfOldKey.closedWith(t, 5*time.Second))
}
