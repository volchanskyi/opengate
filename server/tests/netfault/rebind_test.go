// A customer's router reboots at 3 a.m. and every machine returns on a new
// address. The shaper reproduces that by moving its server-facing sockets while
// leaving the address its machines dial alone.
package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Re-addressing is the scenario's whole mechanism: the machine's path arrives
// at the server from a port it has never used before, mid-connection, with the
// session otherwise untouched.
func TestRebindMovesEveryServerFacingSocket(t *testing.T) {
	t.Parallel()
	shaper, addr, server := startShaper(t, 1)
	conn := machine(t, addr)

	_, err := exchange(t, conn, "before")
	require.NoError(t, err)
	require.Equal(t, 1, server.sources())

	require.NoError(t, shaper.Rebind())

	got, err := exchange(t, conn, "after")
	require.NoError(t, err, "the machine's path did not survive the re-addressing")
	assert.Equal(t, "echo:after", got)
	assert.Equal(t, 2, server.sources(),
		"the server saw the same source address after a re-addressing")
	assert.Equal(t, int64(1), shaper.Counters().Rebinds)
}

// The machine keeps its own address across a rebind. Moving both ends would be
// two changes at once, and the scenario is about the path the server sees.
func TestRebindLeavesTheMachineFacingAddressAlone(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)
	_, err := exchange(t, conn, "before")
	require.NoError(t, err)

	require.NoError(t, shaper.Rebind())
	assert.Equal(t, addr.String(), shaper.ListenAddr().String(),
		"the address the machine dials moved with the re-addressing")
}
