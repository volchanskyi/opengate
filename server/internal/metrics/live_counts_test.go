package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// The three runtime counts are the process's own tallies of what it is holding
// right now — machines connected, relay sessions open, AMT devices attached.
// Each is a single value the process already has, so the exposition works it out
// when it is read.
//
// A copy refreshed on a timer answers a question about the past wearing the
// present's clothes, and nothing downstream can tell how old the answer is. A
// load run comparing its own count of the fleet against the server's read a
// shortfall proportional to how fast machines were arriving, on twenty-five
// phases across nine legs, while the goroutine count taken in the same read —
// which is worked out when asked — agreed with the run exactly. A run holding
// five hundred machines was refused for a server "holding" four hundred and
// fifty-eight.

// gathered is the value of one series on the page, and whether the page carried
// it at all.
func gathered(t *testing.T, reg *prometheus.Registry, name string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		require.Len(t, family.GetMetric(), 1, "%s is a single series", name)
		require.Equal(t, dto.MetricType_GAUGE, family.GetType(), "%s is a gauge", name)
		return family.GetMetric()[0].GetGauge().GetValue(), true
	}
	return 0, false
}

// A count is the tally at the instant the page is read, so a machine that
// arrived a moment ago is on the page a moment later rather than up to one
// refresh interval later.
func TestRuntimeCountsAreReadWhenThePageIsRead(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	agents, sessions, devices := 0, 0, 0
	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return sessions },
		ConnectedAgents:     func() int { return agents },
		ConnectedMPSDevices: func() int { return devices },
	}))

	value, carried := gathered(t, reg, "opengate_agents_connected")
	require.True(t, carried, "a bound count is on the page")
	require.Equal(t, 0.0, value, "a fleet of nothing is nought rather than absent")

	agents, sessions, devices = 8000, 5, 3

	value, carried = gathered(t, reg, "opengate_agents_connected")
	require.True(t, carried)
	require.Equal(t, 8000.0, value, "the page carries the fleet the process is holding now")

	value, _ = gathered(t, reg, "opengate_relay_active_sessions")
	require.Equal(t, 5.0, value)
	value, _ = gathered(t, reg, "opengate_mps_connected_devices")
	require.Equal(t, 3.0, value)
}

// A count nobody can take is not a count of nought. Until the product is
// assembled there is nothing to ask, and a zero on the page would say the fleet
// is empty — which is a reading, and a wrong one.
func TestRuntimeCountsAreAbsentUntilThereIsSomethingToAsk(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	NewMetrics(reg)

	for _, name := range []string{
		"opengate_agents_connected",
		"opengate_relay_active_sessions",
		"opengate_mps_connected_devices",
	} {
		_, carried := gathered(t, reg, name)
		require.False(t, carried, "%s says nothing until it has something to ask", name)
	}
}

// Binding again replaces what is asked rather than publishing the series twice,
// so a process assembled once and re-bound — which a test harness does — still
// answers with one number per series.
func TestBindingRuntimeCountsAgainReplacesWhatIsAsked(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return 1 },
		ConnectedAgents:     func() int { return 1 },
		ConnectedMPSDevices: func() int { return 1 },
	}))
	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return 2 },
		ConnectedAgents:     func() int { return 2 },
		ConnectedMPSDevices: func() int { return 2 },
	}))

	value, carried := gathered(t, reg, "opengate_agents_connected")
	require.True(t, carried)
	require.Equal(t, 2.0, value, "the latest binding is the one that answers")
}

// A binding with a hole in it is refused rather than half-applied: a source
// missing one callback would publish two of the three series and leave the
// third silently absent, which reads as a metric nobody exports.
func TestBindingRuntimeCountsRefusesASourceWithAHoleInIt(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.Error(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:  func() int { return 1 },
		ConnectedAgents: func() int { return 1 },
	}), "a source missing a callback is refused")

	_, carried := gathered(t, reg, "opengate_agents_connected")
	require.False(t, carried, "a refused binding leaves nothing bound")
}
