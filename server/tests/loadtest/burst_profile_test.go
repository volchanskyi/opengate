package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestTheBurstProfileHoldsTheNightToBothCases(t *testing.T) {
	p, err := LoadProfile("../../../load/profiles/burst.yaml")
	require.NoError(t, err)

	assert.Equal(t, FamilySpike, p.Family)
	assert.Equal(t, EnvRunner, p.Environment)
	require.Len(t, p.Phases, 2)
	back, inventory := p.Phases[0], p.Phases[1]
	assert.Equal(t, 5000, back.ConnectedAgents, "production's fleet returns")
	assert.LessOrEqual(t, back.Duration.Duration, 30*time.Second, "at once, as after a release restart")
	assert.Equal(t, 20, back.LogPages, "while twenty technicians each open a log page")
	assert.Equal(t, protocol.MaxMachineMessageBytes, inventory.InventoryBytes,
		"then every machine sends a message at the server's ceiling")
	assert.Equal(t, 5*time.Minute, inventory.Duration.Duration, "and the recovery is judged on five minutes")
	assert.True(t, inventory.Measured)

	blocking := map[string]Gate{}
	for _, gate := range p.Gates {
		if gate.Blocking {
			blocking[gate.Measurement()] = gate
		}
	}
	for measurement, max := range map[string]float64{
		"quic/quic-agents/aggregate|error_rate": 0,
		"quic/online/return|online_all_seconds": 300,
		"k6/journeys/log-page|latency_max_ms":   30000,
	} {
		gate, held := blocking[measurement]
		require.Truef(t, held, "%s fails the night", measurement)
		require.NotNil(t, gate.Max, measurement)
		assert.InDelta(t, max, *gate.Max, 0, measurement)
	}
	for measurement, min := range map[string]float64{
		"k6/journeys/log-page|requests":              20,
		"burst/inventory/inventory|inventory_sent":   5000,
		"burst/inventory/inventory|inventory_stored": 5000,
		"catchup/live/drain|within_5s_share":         0.99,
	} {
		gate, held := blocking[measurement]
		require.Truef(t, held, "%s fails the night", measurement)
		require.NotNil(t, gate.Min, measurement)
		assert.InDelta(t, min, *gate.Min, 0, measurement)
	}
}
