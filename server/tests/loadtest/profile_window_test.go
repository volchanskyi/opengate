package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func windowProfileYAML(rampMeasured, steadyMeasured bool) []byte {
	mark := func(measured bool) string {
		if measured {
			return "\n    measured: true"
		}
		return ""
	}
	return []byte(fmt.Sprintf(`
schema_version: 1
name: normal
family: normal
environment: staging
fixture: small
phases:
  - name: ramp
    duration: 30s
    operator_arrivals_per_second: 2
    connected_agents: 100
    sessions: 0%s
  - name: steady
    duration: 2m
    operator_arrivals_per_second: 5
    connected_agents: 500
    sessions: 5%s
safety:
  max_node_cpu_percent: 85
  max_node_memory_percent: 90
  max_error_rate: 0.01
`, mark(rampMeasured), mark(steadyMeasured)))
}

func TestProfileNamesTheWindowItsNumbersAreTakenOver(t *testing.T) {
	t.Run("a phase can be marked, and the profile says which", func(t *testing.T) {
		p, err := ParseProfile(windowProfileYAML(false, true))
		require.NoError(t, err)
		assert.False(t, p.Phases[0].Measured)
		assert.True(t, p.Phases[1].Measured)

		window := p.MeasuredPhase()
		require.NotNil(t, window)
		assert.Equal(t, "steady", window.Name)
	})

	t.Run("a profile that marks none measures the whole run", func(t *testing.T) {
		p, err := ParseProfile([]byte(minimalProfile))
		require.NoError(t, err)
		assert.Nil(t, p.MeasuredPhase(), "no window named means the run is the window")
	})

	t.Run("two windows are no window", func(t *testing.T) {
		_, err := ParseProfile(windowProfileYAML(true, true))
		require.Error(t, err, "percentiles pooled across two loads describe neither")
		assert.Contains(t, err.Error(), "measured")
	})
}
