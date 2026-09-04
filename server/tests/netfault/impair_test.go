// What the shaper decides about one datagram: whether it is carried, dropped,
// or held — and whether two nights with the same seed decide it the same way.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPassForwardsEverythingUntouched(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{})

	for i := range 100 {
		for _, dir := range []Direction{ToServer, ToMachine} {
			v := imp.Decide(dir, datagramBytes, base.Add(time.Duration(i)*time.Millisecond))
			assert.False(t, v.Drop, "the pass-through profile dropped a datagram")
			assert.Zero(t, v.Delay, "the pass-through profile delayed a datagram")
		}
	}
}

func TestBlackholeDropsBothDirections(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{Blackhole: true})

	for _, dir := range []Direction{ToServer, ToMachine} {
		v := imp.Decide(dir, datagramBytes, base)
		assert.Truef(t, v.Drop, "blackhole let a datagram through toward %s", dir)
	}
}

// A one-way loss must leave the other way alone. A symmetric fault would hide
// which side the recovery machinery is coping with, which is the whole point of
// stating the direction.
func TestLossIsOneDirectionOnly(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{LossToServer: 1.0})

	assert.True(t, imp.Decide(ToServer, datagramBytes, base).Drop,
		"a loss fraction of 1 toward the server let a datagram through")
	assert.False(t, imp.Decide(ToMachine, datagramBytes, base).Drop,
		"a loss fraction toward the server dropped a datagram toward the machine")
}

// The fraction has to be the fraction. A generator that drops a fifth of the
// datagrams somewhere between a tenth and a third produces a scenario nobody
// can compare against last night's.
func TestLossFractionIsTheFractionAsked(t *testing.T) {
	t.Parallel()
	const (
		datagrams = 20000
		asked     = 0.20
		tolerance = 0.01
	)
	imp := NewImpairer(7)
	imp.Set(Profile{LossToServer: asked})

	dropped := 0
	for i := range datagrams {
		if imp.Decide(ToServer, datagramBytes, base.Add(time.Duration(i)*time.Millisecond)).Drop {
			dropped++
		}
	}

	got := float64(dropped) / float64(datagrams)
	assert.InDelta(t, asked, got, tolerance,
		"asked for %.0f%% loss and got %.2f%%", asked*100, got*100)
}

// Two nights with the same seed drop the same datagrams. Without this the trend
// compares a run against a differently-unlucky one and calls the difference a
// regression.
func TestSameSeedDropsTheSameDatagrams(t *testing.T) {
	t.Parallel()
	decisions := func(seed uint64) []bool {
		imp := NewImpairer(seed)
		imp.Set(Profile{LossToServer: 0.3, LossToMachine: 0.3})
		out := make([]bool, 0, 400)
		for i := range 200 {
			at := base.Add(time.Duration(i) * time.Millisecond)
			out = append(out, imp.Decide(ToServer, datagramBytes, at).Drop)
			out = append(out, imp.Decide(ToMachine, datagramBytes, at).Drop)
		}
		return out
	}

	assert.Equal(t, decisions(42), decisions(42), "the same seed produced different drops")
	assert.NotEqual(t, decisions(42), decisions(43), "two different seeds produced identical drops")
}

// Each direction draws from its own generator, so a datagram arriving one way
// cannot change which datagram is dropped the other way. Sharing one generator
// would make every measurement depend on the interleaving of two independent
// arrival streams, which is the one thing a drill cannot reproduce.
func TestDirectionsDrawIndependently(t *testing.T) {
	t.Parallel()
	withInterference := NewImpairer(11)
	withInterference.Set(Profile{LossToServer: 0.5, LossToMachine: 0.5})
	alone := NewImpairer(11)
	alone.Set(Profile{LossToServer: 0.5, LossToMachine: 0.5})

	var interleaved, isolated []bool
	for i := range 100 {
		at := base.Add(time.Duration(i) * time.Millisecond)
		interleaved = append(interleaved, withInterference.Decide(ToServer, datagramBytes, at).Drop)
		withInterference.Decide(ToMachine, datagramBytes, at)
		isolated = append(isolated, alone.Decide(ToServer, datagramBytes, at).Drop)
	}
	assert.Equal(t, isolated, interleaved,
		"traffic toward the machine changed which datagrams were dropped toward the server")
}

func TestDelayHoldsEachDatagramBothWays(t *testing.T) {
	t.Parallel()
	imp := NewImpairer(1)
	imp.Set(Profile{DelayEachWay: 300 * time.Millisecond})

	for _, dir := range []Direction{ToServer, ToMachine} {
		v := imp.Decide(dir, datagramBytes, base)
		assert.False(t, v.Drop)
		assert.Equal(t, 300*time.Millisecond, v.Delay,
			"the satellite-style delay was not applied toward %s", dir)
	}
}
