package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

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

func gatheredCounter(t *testing.T, reg *prometheus.Registry, name string) (float64, bool) {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		require.Len(t, family.GetMetric(), 1, "%s is a single series", name)
		require.Equal(t, dto.MetricType_COUNTER, family.GetType(), "%s is a counter", name)
		return family.GetMetric()[0].GetCounter().GetValue(), true
	}
	return 0, false
}

func TestRuntimeCountsAreReadWhenThePageIsRead(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	agents, sessions, devices := 0, 0, 0
	var started uint64
	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return sessions },
		SessionsStarted:     func() uint64 { return started },
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

	total, carried := gatheredCounter(t, reg, "opengate_relay_sessions_started_total")
	require.True(t, carried, "the started count is on the page beside the open count")
	require.Equal(t, 0.0, total, "no session yet is nought rather than absent")
	started = 41
	total, _ = gatheredCounter(t, reg, "opengate_relay_sessions_started_total")
	require.Equal(t, 41.0, total, "the page carries every session started so far")
}

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
	_, carried := gatheredCounter(t, reg, "opengate_relay_sessions_started_total")
	require.False(t, carried, "the started count says nothing until it has something to ask")
}

func TestBindingRuntimeCountsAgainReplacesWhatIsAsked(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return 1 },
		SessionsStarted:     func() uint64 { return 1 },
		ConnectedAgents:     func() int { return 1 },
		ConnectedMPSDevices: func() int { return 1 },
	}))
	require.NoError(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return 2 },
		SessionsStarted:     func() uint64 { return 2 },
		ConnectedAgents:     func() int { return 2 },
		ConnectedMPSDevices: func() int { return 2 },
	}))

	value, carried := gathered(t, reg, "opengate_agents_connected")
	require.True(t, carried)
	require.Equal(t, 2.0, value, "the latest binding is the one that answers")
}

func TestBindingRuntimeCountsRefusesASourceWithAHoleInIt(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)

	require.Error(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:  func() int { return 1 },
		ConnectedAgents: func() int { return 1 },
	}), "a source missing a callback is refused")
	require.Error(t, m.BindRuntimeCounts(GaugeSource{
		ActiveSessions:      func() int { return 1 },
		ConnectedAgents:     func() int { return 1 },
		ConnectedMPSDevices: func() int { return 1 },
	}), "a source with no started count is refused")

	_, carried := gathered(t, reg, "opengate_agents_connected")
	require.False(t, carried, "a refused binding leaves nothing bound")
}
