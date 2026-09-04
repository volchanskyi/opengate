// What the shaper reports about itself, and what it does when it is stood up
// and taken down. The counters are what tell the runner a scenario ran at all.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scenario whose drop count does not match its instruction did not run. The
// counters are what the runner reads at each phase boundary to know it did.
func TestShaperAccountsForEveryDatagramItHandles(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)

	_, err := exchange(t, conn, "one")
	require.NoError(t, err)

	clear := shaper.Counters()
	assert.Equal(t, int64(1), clear.ToServer.In)
	assert.Equal(t, int64(1), clear.ToServer.Out)
	assert.Zero(t, clear.ToServer.Dropped)
	assert.Equal(t, int64(1), clear.ToMachine.In)
	assert.Equal(t, int64(1), clear.ToMachine.Out)

	require.NoError(t, shaper.SetProfile(Profile{Blackhole: true}))
	_, err = conn.Write([]byte("two"))
	require.NoError(t, err)

	require.Eventually(t, func() bool { return shaper.Counters().ToServer.Dropped == 1 },
		readDeadline, 10*time.Millisecond, "the shaper did not count the datagram it dropped")

	dark := shaper.Counters()
	assert.Equal(t, int64(2), dark.ToServer.In, "a dropped datagram was not counted as arriving")
	assert.Equal(t, int64(1), dark.ToServer.Out, "a dropped datagram was counted as forwarded")
}

// The counters carry the seed, so the evidence bundle records which run of the
// generator produced the night's drops without the runner having to be told.
func TestCountersCarryTheSeedAndTheProfileInForce(t *testing.T) {
	t.Parallel()
	shaper, _, _ := startShaper(t, 99)
	require.NoError(t, shaper.SetProfile(Profile{LossToServer: 0.2}))

	got := shaper.Counters()
	assert.Equal(t, uint64(99), got.Seed)
	assert.InDelta(t, 0.2, got.Profile.LossToServer, 0)
}

// A profile the shaper cannot honour is refused at the door, so a scenario
// that mistyped its instruction fails loudly rather than running as whatever
// the shaper made of it.
func TestShaperRefusesAnImpossibleProfile(t *testing.T) {
	t.Parallel()
	shaper, _, _ := startShaper(t, 1)
	assert.Error(t, shaper.SetProfile(Profile{LossToServer: 2}))
}

// Closing must not leave a timer about to write into a socket the shaper has
// already released. This is the shape that would surface as a panic in a
// teardown nobody reads the logs of.
func TestCloseWaitsForWhatIsStillOnTheWire(t *testing.T) {
	t.Parallel()
	shaper, addr, _ := startShaper(t, 1)
	conn := machine(t, addr)
	require.NoError(t, shaper.SetProfile(Profile{DelayEachWay: 250 * time.Millisecond}))

	_, err := conn.Write([]byte("in flight"))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return shaper.Counters().ToServer.In == 1 },
		readDeadline, time.Millisecond, "the shaper never read the datagram")

	shaper.Close()
	shaper.Close() // closing twice is what a failed run's teardown does
}

// A shaper that cannot be stood up says so. The alternative is a drill that
// runs its scenarios against a forwarder that never bound anything and reports
// every one of them as a total outage.
func TestNewShaperRefusesAnAddressItCannotUse(t *testing.T) {
	t.Parallel()
	cases := map[string]Config{
		"a server address that is not one": {Listen: "127.0.0.1:0", ServerAddr: "not an address"},
		"a listen address that is not one": {Listen: "not an address", ServerAddr: "127.0.0.1:9090"},
		"a port this process cannot have":  {Listen: "127.0.0.1:1", ServerAddr: "127.0.0.1:9090"},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			shaper, err := NewShaper(cfg)
			assert.Error(t, err, "an unusable address was accepted")
			assert.Nil(t, shaper)
		})
	}
}
