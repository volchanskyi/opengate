package agentapi

import (
	"context"
	"crypto/tls"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/certledger"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/testutil/machinecert"
)

const (
	day       = 24 * time.Hour
	leafLife  = 90 * day
	graceLife = 90 * day
)

func TestADeletedMachineReadsItsRemovalOrderBeforeTheClose(t *testing.T) {
	for _, fastPath := range []bool{false, true} {
		t.Run(map[bool]string{false: "full handshake", true: "fast path"}[fastPath], func(t *testing.T) {
			env := newCertEnv(t, "")
			id, machine := env.machine(t)
			env.srv.DeregisterAgent(context.Background(), id)

			var m *machineConn
			if fastPath {
				m = env.dialFast(t, machine, env.cm.IssuerHash())
				m.register(t)
			} else {
				m = env.dial(t, machine)
			}
			order := m.await(t, protocol.MsgAgentDeregistered, 5*time.Second)
			assert.Equal(t, "device deleted by administrator", order.Reason)

			started := time.Now()
			_ = m.conn.CloseWithError(0, "bye")
			assert.Less(t, time.Since(started), time.Second)
		})
	}
}

func TestADeletedMachineThatNeverHangsUpIsClosedAfterTheWait(t *testing.T) {
	t.Parallel()
	env := newCertEnv(t, "")
	id, machine := env.machine(t)
	env.srv.DeregisterAgent(context.Background(), id)

	m := env.dial(t, machine)
	m.await(t, protocol.MsgAgentDeregistered, 5*time.Second)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseDeviceDeleted), m.closedWith(t, removalOrderWait+2*time.Second))
}

func TestUnlistedCertificateRefused(t *testing.T) {
	env := newCertEnv(t, "")
	id, _ := env.machine(t)
	forged := deviceCert(t, env.cm, id)

	m := env.dial(t, forged)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), m.closedWith(t, 5*time.Second),
		"a certificate the server's own master key signed, but never recorded, is refused")
	assert.Equal(t, 0, env.srv.ConnectedAgentCount())
}

func TestSupersededCertRefused(t *testing.T) {
	env := newCertEnv(t, "")
	id, old := env.machine(t)
	_, err := env.issuer.Ledger().MarkInstalled(context.Background(),
		certledger.FingerprintOf(old.Certificate[0]), env.clock.Now())
	require.NoError(t, err)
	current := machineReissue(t, env, id)

	m := env.dial(t, old)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), m.closedWith(t, 5*time.Second))
	env.dial(t, current).register(t)
}

// machineReissue records a later certificate for the device and makes it current, as a renewal
// the machine installed does.
func machineReissue(t *testing.T, env *certEnv, id uuid.UUID) *tls.Certificate {
	t.Helper()
	env.clock.Advance(time.Minute)
	renewed := machinecert.Issue(t, env.issuer, id)
	fp := certledger.FingerprintOf(renewed.Certificate[0])
	_, err := env.issuer.Ledger().MarkInstalled(context.Background(), fp, env.clock.Now())
	require.NoError(t, err)
	return renewed
}

func TestTombstonedExpiredOrTooOldGetsRemovalOrder(t *testing.T) {
	for name, age := range map[string]time.Duration{
		"expired":  leafLife + day,
		"too old":  leafLife + graceLife + day,
		"in force": time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			env := newCertEnv(t, "")
			id, machine := env.machine(t)
			env.clock.Advance(age)
			env.srv.DeregisterAgent(context.Background(), id)

			m := env.dial(t, machine)
			m.await(t, protocol.MsgAgentDeregistered, 5*time.Second)
		})
	}
}

func TestExpiredClientCertCompletesTLSThenIsRefused(t *testing.T) {
	env := newCertEnv(t, "")
	_, machine := env.machine(t)
	env.clock.Advance(leafLife + graceLife + day)

	m := env.dial(t, machine)
	assert.Equal(t, quic.ApplicationErrorCode(protocol.CloseCertificateRefused), m.closedWith(t, 5*time.Second),
		"past the grace the machine is told it is refused, after TLS let it in to hear so")
}

func TestForeignClientCertFailsTLS(t *testing.T) {
	env := newCertEnv(t, "")
	other := newCertEnv(t, "")
	_, foreign := other.machine(t)

	// The client finishes its side first, so the server's alert may arrive just after the dial.
	conn, err := quic.DialAddr(t.Context(), env.addr, env.tlsFor(foreign), &quic.Config{})
	if err == nil {
		select {
		case <-conn.Context().Done():
		case <-time.After(5 * time.Second):
			t.Fatal("a foreign certificate was let in")
		}
		err = context.Cause(conn.Context())
	}
	var transportErr *quic.TransportError
	require.True(t, errors.As(err, &transportErr), "a TLS alert, not an application close: %v", err)
	assert.True(t, transportErr.ErrorCode.IsCryptoError())
}
