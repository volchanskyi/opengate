package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShaperForwardsBothWaysUntouched(t *testing.T) {
	t.Parallel()
	_, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)

	got, err := exchange(t, conn, "hello")
	require.NoError(t, err)
	assert.Equal(t, "echo:hello", got, "the shaper altered what it forwarded")
}

func TestShaperHoldsOneServerSocketPerMachine(t *testing.T) {
	t.Parallel()
	shaper, addr, server := startShaper(t, 1)

	for i, payload := range []string{"one", "two", "three"} {
		conn := machine(t, addr)
		got, err := exchange(t, conn, payload)
		require.NoErrorf(t, err, "machine %d got no reply", i)
		require.Equal(t, "echo:"+payload, got, "machine %d got another machine's reply", i)
	}

	assert.Equal(t, 3, server.sources(), "the server did not see one source per machine")
	assert.Equal(t, 3, shaper.Machines(), "the shaper did not hold one mapping per machine")
}

func TestShaperReusesAMachinesMapping(t *testing.T) {
	t.Parallel()
	shaper, addr, server := startShaper(t, 1)
	conn := machine(t, addr)

	for range 5 {
		_, err := exchange(t, conn, "again")
		require.NoError(t, err)
	}
	assert.Equal(t, 1, shaper.Machines())
	assert.Equal(t, 1, server.sources(), "one machine's traffic reached the server from several addresses")
}

func TestATruncatedReadIsFatalRatherThanQuiet(t *testing.T) {
	t.Parallel()
	assert.NoError(t, checkRead(readBufferBytes-1, readBufferBytes))
	assert.Error(t, checkRead(readBufferBytes, readBufferBytes),
		"a read that filled the buffer exactly was accepted as a whole datagram")
}

func TestReadBufferHoldsTheLargestDatagramThereIs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 64*1024, readBufferBytes)
}

func TestBlackholeStopsTrafficBothWays(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)

	// The mapping forms while the link is clear, so the outage interrupts a live path.
	_, err := exchange(t, conn, "before")
	require.NoError(t, err)

	require.NoError(t, shaper.SetProfile(Profile{Blackhole: true}))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))
	_, err = conn.Write([]byte("during"))
	require.NoError(t, err)
	_, err = conn.Read(make([]byte, readBufferBytes))
	assert.Error(t, err, "a datagram crossed a blackholed shaper")

	require.NoError(t, shaper.SetProfile(Profile{}))
	got, err := exchange(t, conn, "after")
	require.NoError(t, err, "the link did not come back when the outage lifted")
	assert.Equal(t, "echo:after", got)
}

func TestDelayedDatagramsStillArrive(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)
	require.NoError(t, shaper.SetProfile(Profile{DelayEachWay: 50 * time.Millisecond}))

	sent := time.Now()
	got, err := exchange(t, conn, "slow")
	require.NoError(t, err, "a delayed datagram never arrived")
	assert.Equal(t, "echo:slow", got)
	assert.GreaterOrEqual(t, time.Since(sent), 100*time.Millisecond,
		"the round trip was quicker than the delay applied to each half of it")
	awaitForwarded(t, shaper, 1, 1)
}
