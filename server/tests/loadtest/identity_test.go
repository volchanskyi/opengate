package main

import (
	"context"
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Which machine a credential belongs to.
//
// The server knows a machine by its certificate and takes its identifier from
// that certificate's common name, which the harness chose when it enrolled. That
// is what lets a run file its estate without asking the server which machines
// exist — the answer is already in the credential each machine dials with.

// The estate can be filed without asking the server what machines it has.
//
// The harness chooses each machine's identifier when it enrols and puts it in
// the certificate's common name, which is the field the server parses back out
// on every connection — so the identifier the run needs in order to file a
// machine is already in the credential that machine dials with. The alternative
// is listing the fleet back and matching it up by name, which is a second
// source of truth for something the run already knows.
func TestAMachinesIdentifierIsReadableFromTheCredentialItDialsWith(t *testing.T) {
	credentials, err := newAgentCredentials(t.TempDir(), "", "")
	require.NoError(t, err)

	config, err := credentials.forAgent(context.Background(), tenantAgent{hostname: "soak-t0-a0"})
	require.NoError(t, err)

	deviceID, ok := deviceIDFrom(config)
	require.True(t, ok, "a machine that holds a certificate has an identifier")
	assert.NotEmpty(t, deviceID)

}

// The identifier a machine is filed under is the one it keeps.
//
// A run holds its identities through enrolOnce, which is what makes a machine
// that comes back the same machine. Reading the identifier through that same
// wrapper is what the filing does, and it has to answer the same way twice: an
// identifier minted fresh on each look would file one machine repeatedly under
// names the server has never seen, growing the customer's list for as long as
// the run lasts.
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

// A credential with nothing in it is not a machine with an empty name. Reading
// one as the other would file a machine under an identifier the server has
// never issued, and the refusal would name the wrong thing.
func TestACredentialWithNoCertificateHasNoIdentifier(t *testing.T) {
	_, ok := deviceIDFrom(&tls.Config{MinVersion: tls.VersionTLS13})
	assert.False(t, ok)

	_, ok = deviceIDFrom(nil)
	assert.False(t, ok)
}
