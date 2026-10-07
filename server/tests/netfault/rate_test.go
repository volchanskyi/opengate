package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	// 100 ms of a 2 Mbit/s link carries 20 datagrams, and the first meets an empty link, so 21 fit.
	assert.Equal(t, 21, queued, "the queue admitted the wrong number of datagrams")
	assert.Equal(t, 179, dropped, "the link did not tail-drop past its queue depth")
}

func TestRateQueueDrainsWithTime(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{RateBitsPerSec: 2_000_000, MaxQueue: 100 * time.Millisecond})

	for range 21 {
		require.False(t, imp.Decide(ToServer, datagramBytes, base).Drop)
	}
	require.True(t, imp.Decide(ToServer, datagramBytes, base).Drop,
		"the queue was not full when the test expected it to be")

	v := imp.Decide(ToServer, datagramBytes, base.Add(time.Second))
	assert.False(t, v.Drop, "the link had not drained after a second of idleness")
	assert.Equal(t, 4800*time.Microsecond, v.Delay, "the drained link still carried a backlog")
}

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
