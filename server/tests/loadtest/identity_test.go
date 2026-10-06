package main

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAMachinesIdentifierIsReadableFromTheCredentialItDialsWith(t *testing.T) {
	credentials, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)

	deviceID, ok := deviceIDFrom(config)
	require.True(t, ok, "a machine that holds a certificate has an identifier")
	assert.NotEmpty(t, deviceID)

}

func TestTheIdentifierAMachineIsFiledUnderIsTheOneItKeeps(t *testing.T) {
	source, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)
	held := enrolOnce(source)

	machine := tenantAgent{hostname: "soak-t0-a0"}
	first, err := held.forAgent(context.Background(), machine)
	require.NoError(t, err)
	firstID, ok := deviceIDFrom(first)
	require.True(t, ok)

	again, err := held.forAgent(context.Background(), machine)
	require.NoError(t, err)
	sameID, ok := deviceIDFrom(again)
	require.True(t, ok)

	assert.Equal(t, firstID, sameID)
}

func TestACredentialWithNoCertificateHasNoIdentifier(t *testing.T) {
	_, ok := deviceIDFrom(&tls.Config{MinVersion: tls.VersionTLS13})
	assert.False(t, ok)

	_, ok = deviceIDFrom(nil)
	assert.False(t, ok)
}
