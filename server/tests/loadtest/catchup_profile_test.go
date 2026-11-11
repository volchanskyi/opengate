package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheCatchUpProfileHoldsTheNightToTheCatchUpPromises(t *testing.T) {
	p, err := LoadProfile("../../../load/profiles/catch-up.yaml")
	require.NoError(t, err)

	assert.Equal(t, FamilySpike, p.Family)
	assert.Equal(t, EnvRunner, p.Environment)
	require.Len(t, p.Phases, 2)
	assert.Equal(t, 5000, p.Phases[0].ConnectedAgents, "decision E's five thousand machines come back")
	assert.LessOrEqual(t, p.Phases[0].Duration.Duration, time.Minute, "at once, as a site's link returns")
	assert.Equal(t, 5000, p.Phases[1].ConnectedAgents, "and stay while they drain")
	assert.True(t, p.Phases[1].Measured)

	peak, err := LoadProfile("../../../load/profiles/peak.yaml")
	require.NoError(t, err)
	assert.Equal(t, peak.Phases[1].Sessions, p.Phases[1].Sessions,
		"technician sessions are held at the busy morning's level while the fleet catches up")

	blocking := map[string]Gate{}
	for _, gate := range p.Gates {
		if gate.Blocking {
			blocking[gate.Measurement()] = gate
		}
	}
	live := blocking["catchup/live/drain|within_5s_share"]
	require.NotNil(t, live.Min, "99% of live readings are accepted within 5 s, or the night fails")
	assert.InDelta(t, 0.99, *live.Min, 0)
	caught := blocking["catchup/gaps/drain|machines_caught_up"]
	require.NotNil(t, caught.Min, "every machine's gap is filled")
	assert.InDelta(t, 5000, *caught.Min, 0)
	fill := blocking["catchup/gaps/drain|fill_seconds"]
	require.NotNil(t, fill.Max, "within fifteen minutes of the fleet's return minute")
	assert.InDelta(t, 960, *fill.Max, 0)
	for _, measurement := range []string{"quic/quic-agents/aggregate|error_rate", "quic/quic-agents/connect|latency_p95_ms"} {
		want := peakGate(t, peak, measurement)
		got, held := blocking[measurement]
		require.Truef(t, held, "%s is held to the busy morning's limit", measurement)
		assert.Equal(t, *want.Max, *got.Max, measurement)
	}
}

func peakGate(t *testing.T, peak *Profile, measurement string) Gate {
	t.Helper()
	for _, gate := range peak.Gates {
		if gate.Blocking && gate.Measurement() == measurement {
			return gate
		}
	}
	t.Fatalf("the busy morning holds no limit on %s", measurement)
	return Gate{}
}
