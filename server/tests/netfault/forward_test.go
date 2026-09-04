// The forwarder over real sockets: what reaches the server, what comes back,
// and which machine each reply belongs to.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The instrument must not change what it measures. A shaper told to pass
// carries a datagram both ways unaltered, or every number the drill produces
// describes the shaper.
func TestShaperForwardsBothWaysUntouched(t *testing.T) {
	t.Parallel()
	_, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)

	got, err := exchange(t, conn, "hello")
	require.NoError(t, err)
	assert.Equal(t, "echo:hello", got, "the shaper altered what it forwarded")
}

// One server-facing socket per machine, so the server sees a distinct source
// per machine and each reply routes back to the machine that asked. Sharing one
// socket would deliver every reply to whichever machine spoke last.
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

// A machine that keeps talking keeps its mapping. Minting a fresh one per
// datagram would give the server a new source address per packet, which is a
// re-addressing scenario nobody asked for.
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

// Go's ReadFromUDP truncates silently. A truncated path-probing packet would
// read as corruption the drill never asked for, and the run would report a
// finding about the product that belongs to the instrument.
func TestATruncatedReadIsFatalRatherThanQuiet(t *testing.T) {
	t.Parallel()
	assert.NoError(t, checkRead(readBufferBytes-1, readBufferBytes))
	assert.Error(t, checkRead(readBufferBytes, readBufferBytes),
		"a read that filled the buffer exactly was accepted as a whole datagram")
}

// 64 KiB is the largest a datagram can be, so a read that fills it is the only
// reading that cannot be told apart from a truncation.
func TestReadBufferHoldsTheLargestDatagramThereIs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 64*1024, readBufferBytes)
}

// A blackholed shaper is dark, not slow. The scenario asks for the connection
// to die at the idle timeout, which needs nothing to arrive at all.
func TestBlackholeStopsTrafficBothWays(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)

	// The mapping is established while the link is clear, so the outage
	// interrupts a live path rather than preventing one from forming.
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

// A delayed datagram still arrives. The satellite scenario is the one where a
// forwarder that quietly dropped what it was told to hold would look exactly
// like a working link with a very patient agent on the end of it.
func TestDelayedDatagramsStillArrive(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)
	require.NoError(t, shaper.SetProfile(Profile{DelayEachWay: 50 * time.Millisecond}))

	sent := time.Now()
	got, err := exchange(t, conn, "slow")
	require.NoError(t, err, "a delayed datagram never arrived")
	assert.Equal(t, "echo:slow", got)
	// Both ways are delayed, so the round trip carries two of them.
	assert.GreaterOrEqual(t, time.Since(sent), 100*time.Millisecond,
		"the round trip was quicker than the delay applied to each half of it")
	assert.Equal(t, int64(1), shaper.Counters().ToServer.Out,
		"a delayed datagram was not counted once it was forwarded")
}
