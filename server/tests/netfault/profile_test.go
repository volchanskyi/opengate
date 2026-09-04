// Which impairments the shaper will accept, and which it refuses at the door.
// A scenario that mistyped its instruction has to fail where it was typed,
// rather than running as whatever the shaper made of the number.
package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProfileRefusesNumbersThatAreNotImpairments(t *testing.T) {
	t.Parallel()
	cases := map[string]Profile{
		"loss toward the server above one":   {LossToServer: 1.5},
		"loss toward the machine below zero": {LossToMachine: -0.1},
		"a delay that runs backwards":        {DelayEachWay: -time.Second},
		"a negative rate":                    {RateBitsPerSec: -1},
		"a negative queue depth":             {MaxQueue: -time.Second},
		"a rate with no queue to hold it":    {RateBitsPerSec: 2_000_000},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Error(t, p.Validate(), "an impossible profile was accepted")
		})
	}
}

func TestProfileAcceptsEveryScenarioTheDrillRuns(t *testing.T) {
	t.Parallel()
	cases := map[string]Profile{
		"pass through":     {},
		"total outage":     {Blackhole: true},
		"a thin uplink":    {RateBitsPerSec: 2_000_000, MaxQueue: time.Second},
		"one-way loss":     {LossToServer: 0.2},
		"a satellite link": {DelayEachWay: 300 * time.Millisecond},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, p.Validate())
		})
	}
}

func TestDirectionNamesItselfForTheEvidence(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "to_server", ToServer.String())
	assert.Equal(t, "to_machine", ToMachine.String())
}
