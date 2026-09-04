// The site's shared uplink: one link, every machine on it, a finite queue, and
// a tail drop past the end of it. This is the impairment the thin-uplink
// scenario rests on, so it is the one whose arithmetic is stated exactly.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The token bucket is the site's shared uplink: one link, every machine on it.
// A datagram that does not fit in the link's current capacity waits for the
// link, and the wait it is given is the time the link needs to carry what is
// already queued ahead of it.
func TestRateSerialisesDatagramsOntoOneLink(t *testing.T) {
	t.Parallel()
	// 2 Mbit/s carries a 1200-byte datagram in 4.8 ms.
	const perDatagram = 4800 * time.Microsecond
	imp := NewImpairer(1)
	imp.Set(Profile{RateBitsPerSec: 2_000_000, MaxQueue: time.Second})

	for i := range 10 {
		v := imp.Decide(ToServer, datagramBytes, base)
		require.False(t, v.Drop, "datagram %d was dropped inside the queue depth", i)
		assert.Equal(t, time.Duration(i+1)*perDatagram, v.Delay,
			"datagram %d did not queue behind the ones before it", i)
	}
}

// The uplink is the uplink. Shaping the direction a customer's machines send in
// and leaving the direction they receive in alone is what a site's asymmetric
// connection actually is.
func TestRateShapesTowardTheServerOnly(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{RateBitsPerSec: 2_000_000, MaxQueue: time.Second})

	for range 10 {
		v := imp.Decide(ToMachine, datagramBytes, base)
		assert.False(t, v.Drop)
		assert.Zero(t, v.Delay, "the uplink rate shaped traffic toward the machine")
	}
}

// A real link has a finite queue and drops past the end of it. Without the
// tail drop the shaper would hold an unbounded backlog and report a latency no
// customer's router would ever produce.
func TestRateDropsPastTheQueueDepth(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{RateBitsPerSec: 2_000_000, MaxQueue: 100 * time.Millisecond})

	queued, dropped := 0, 0
	for range 200 {
		if imp.Decide(ToServer, datagramBytes, base).Drop {
			dropped++
		} else {
			queued++
		}
	}
	// 100 ms of a 2 Mbit/s link carries 20 datagrams of 1200 bytes; the first
	// datagram is admitted against an empty link, so 21 fit before the queue is
	// past its depth.
	assert.Equal(t, 21, queued, "the queue admitted the wrong number of datagrams")
	assert.Equal(t, 179, dropped, "the link did not tail-drop past its queue depth")
}

// The link drains. A queue that never empties would turn a scenario's later
// phases into a measurement of the phase before them.
func TestRateQueueDrainsWithTime(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{RateBitsPerSec: 2_000_000, MaxQueue: 100 * time.Millisecond})

	for range 21 {
		require.False(t, imp.Decide(ToServer, datagramBytes, base).Drop)
	}
	require.True(t, imp.Decide(ToServer, datagramBytes, base).Drop,
		"the queue was not full when the test expected it to be")

	// A second later the link has carried everything queued.
	v := imp.Decide(ToServer, datagramBytes, base.Add(time.Second))
	assert.False(t, v.Drop, "the link had not drained after a second of idleness")
	assert.Equal(t, 4800*time.Microsecond, v.Delay, "the drained link still carried a backlog")
}

// Blackhole outranks everything: a scenario that asks for darkness gets it, and
// nothing queues during it to be released the moment it lifts.
func TestBlackholeOutranksTheOtherImpairments(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{Blackhole: true, RateBitsPerSec: 2_000_000, MaxQueue: time.Second, DelayEachWay: time.Second})

	for range 50 {
		v := imp.Decide(ToServer, datagramBytes, base)
		require.True(t, v.Drop)
	}
	imp.Set(Profile{})
	v := imp.Decide(ToServer, datagramBytes, base)
	assert.False(t, v.Drop)
	assert.Zero(t, v.Delay, "darkness left a backlog behind it")
}
