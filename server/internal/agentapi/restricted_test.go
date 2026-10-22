package agentapi

import (
	"context"
	"crypto/x509"
	"net"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestRestrictedConnNotRegistered(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(leafLife + day)

	m := env.dial(t, machine)
	m.register(t)
	m.await(t, protocol.MsgRenewCertificate, 5*time.Second)

	assert.Equal(t, 0, env.srv.ConnectedAgentCount())
	assert.Nil(t, env.srv.GetAgent(id))
	stored, err := env.devices.Get(dbtx.WithDefaultTenant(context.Background(), false), id)
	require.NoError(t, err)
	assert.Equal(t, device.StatusOffline, stored.Status, "a restricted session brings no machine online")
	assert.Equal(t, "machine", stored.Hostname, "a restricted session writes nothing it registers")
}

func TestRestrictedConnClosesAt45s(t *testing.T) {
	t.Parallel()
	env := newCertEnv(t, "")
	_, machine := env.machine(t)
	env.clock.Advance(leafLife + day)

	m := env.dial(t, machine)
	started := time.Now()
	code := m.closedWith(t, restrictedSessionLimit+3*time.Second)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseRestrictedSessionEnded), code)
	assert.InDelta(t, restrictedSessionLimit.Seconds(), time.Since(started).Seconds(), 1.5)
}

func TestRestrictedClosesAfterInstalled(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.clock.Advance(leafLife + day)

	m := env.dial(t, machine)
	m.register(t)
	m.await(t, protocol.MsgRenewCertificate, 5*time.Second)
	csr, _ := renewalRequest(t, id.String())
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateSigningRequest, CSR: csr})
	issued := m.await(t, protocol.MsgCertificateIssued, 5*time.Second)
	assert.NotEmpty(t, issued.Cert)
	m.send(t, &protocol.ControlMessage{Type: protocol.MsgCertificateInstalled})

	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseRestrictedSessionEnded), m.closedWith(t, 5*time.Second))
	assert.True(t, env.issuer.Ledger().Superseded(certledger.FingerprintOf(machine.Certificate[0])),
		"the expired certificate is superseded by the one the machine installed")
}

func TestRestrictedConnNeverTouchesARepository(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	leaf, err := x509.ParseCertificate(machine.Certificate[0])
	require.NoError(t, err)

	types := controlMessageTypes(t)
	require.Equal(t, len(allControlMessageTypes), len(types),
		"a new message type is unlisted here: add it so a restricted connection is tried with it")
	for _, msgType := range allControlMessageTypes {
		server, client := net.Pipe()
		ac := &AgentConn{
			DeviceID:   id,
			stream:     server,
			codec:      &protocol.Codec{},
			logger:     testLogger(),
			restricted: true,
			renewal:    env.srv.renewals,
			leaf:       leaf,
			certFP:     certledger.FingerprintOf(leaf.Raw),
		}
		go drain(client)
		payload, err := ac.codec.EncodeControl(&protocol.ControlMessage{Type: msgType})
		require.NoError(t, err)
		go func() { _ = ac.codec.WriteFrame(client, protocol.FrameControl, payload) }()
		require.NotPanics(t, func() { _ = ac.handleControl(context.Background()) }, "%s", msgType)
		_ = server.Close()
		_ = client.Close()
	}
	assert.Equal(t, 0, env.srv.ConnectedAgentCount())
}

func drain(c net.Conn) {
	buf := make([]byte, 4096)
	for {
		if _, err := c.Read(buf); err != nil {
			return
		}
	}
}
