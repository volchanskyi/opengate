package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestNewerConnectionClosesTheOlderWithCode6(t *testing.T) {
	env := newCertEnv(t, "")
	_, machine := env.machine(t)

	older := env.dial(t, machine)
	older.register(t)
	older.await(t, protocol.MsgTrustBundle, 5*time.Second)
	newer := env.dial(t, machine)
	newer.register(t)

	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseReplacedByNewerConnection),
		older.closedWith(t, livenessProbeWait+3*time.Second))
	newer.await(t, protocol.MsgTrustBundle, 5*time.Second)
	assert.Equal(t, 1, env.srv.ConnectedAgentCount())
}

func TestLiveOlderConnectionFlagsSharedIdentity(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)

	older := env.dial(t, machine)
	older.register(t)
	older.await(t, protocol.MsgTrustBundle, 5*time.Second)
	newer := env.dial(t, machine)
	newer.register(t)
	older.closedWith(t, livenessProbeWait+3*time.Second)

	require.Eventually(t, func() bool {
		stored, err := env.devices.Get(dbtx.WithDefaultTenant(context.Background(), false), id)
		return err == nil && stored.IdentitySharedAt != nil
	}, 5*time.Second, 50*time.Millisecond)
	stored, err := env.devices.Get(dbtx.WithDefaultTenant(context.Background(), false), id)
	require.NoError(t, err)
	assert.Equal(t, []string{"127.0.0.1", "127.0.0.1"}, stored.IdentitySharedAddresses)
}

func TestDeadOlderConnectionFlagsNothing(t *testing.T) {
	env := newCertEnv(t, "")
	id, machine := env.machine(t)

	older := env.dial(t, machine)
	older.register(t)
	older.await(t, protocol.MsgTrustBundle, 5*time.Second)
	// A connection whose reader stopped cannot answer the ping, as a dead link cannot.
	older.stream.CancelRead(0)
	newer := env.dial(t, machine)
	newer.register(t)
	older.closedWith(t, livenessProbeWait+3*time.Second)

	stored, err := env.devices.Get(dbtx.WithDefaultTenant(context.Background(), false), id)
	require.NoError(t, err)
	assert.Nil(t, stored.IdentitySharedAt)
}

func TestSharedIdentityLoggedOncePerHour(t *testing.T) {
	var log hourlyLog
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	assert.True(t, log.due("device", at))
	assert.False(t, log.due("device", at.Add(59*time.Minute)))
	assert.True(t, log.due("other device", at.Add(time.Minute)))
	assert.True(t, log.due("device", at.Add(61*time.Minute)))
}
