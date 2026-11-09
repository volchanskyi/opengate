package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheCatchUpProfileIsAFleetBackFromAnOutageThatGatesNothing(t *testing.T) {
	p, err := LoadProfile("../../../load/profiles/catch-up.yaml")
	require.NoError(t, err)

	assert.Equal(t, FamilySpike, p.Family)
	assert.Equal(t, EnvRunner, p.Environment)
	require.Len(t, p.Phases, 2)
	assert.Equal(t, 5000, p.Phases[0].ConnectedAgents, "decision E's five thousand machines come back")
	assert.LessOrEqual(t, p.Phases[0].Duration.Duration, time.Minute, "at once, as a site's link returns")
	assert.Equal(t, 5000, p.Phases[1].ConnectedAgents, "and stay while they drain")
	assert.True(t, p.Phases[1].Measured)
	for _, gate := range p.Gates {
		assert.False(t, gate.Blocking, "%s is published, never judged, until the shared writer lands", gate.Series)
	}
}
