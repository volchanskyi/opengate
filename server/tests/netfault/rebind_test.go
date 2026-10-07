package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
