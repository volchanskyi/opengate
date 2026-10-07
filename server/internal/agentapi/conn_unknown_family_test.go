package agentapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func familyLabels(samples []telemetry.Sample) []string {
	names := make([]string, 0, len(samples))
	for _, s := range samples {
		if family, ok := s.Labels["family"]; ok {
			names = append(names, family)
		}
	}
	return names
}

// familyIngest feeds one message through handle, flushes, and returns the samples written and
// the metrics that counted the drops.
func familyIngest(
	t *testing.T,
	handle func(*AgentConn, context.Context, *protocol.ControlMessage, int) error,
	msg *protocol.ControlMessage,
	payloadLen int,
) ([]telemetry.Sample, *appmetrics.Metrics) {
	t.Helper()
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	require.NoError(t, handle(ac, tenantCtx(tenant), msg, payloadLen))
	ac.flushTelemetry(tenantCtx(tenant))

	return (<-writer.calls).samples, m
}

func assertOneUnknownFamilyDrop(t *testing.T, m *appmetrics.Metrics) {
	t.Helper()
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_family")), 0)
}

func healthSummaryOf(families []protocol.FamilyAnomalyRate) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		SamplerVersion:  "sysinfo-k2",
		NodeAnomalyRate: 0.125,
		PerFamilyRates:  families,
	}
}

func TestHealthSummaryDropsUnlistedFamilies(t *testing.T) {
	msg := healthSummaryOf([]protocol.FamilyAnomalyRate{
		{Family: "cpu", Rate: 0.25},
		{Family: "process", Rate: 0.5},
		{Family: "proc", Rate: 0.75},
	})
	samples, m := familyIngest(t, (*AgentConn).handleAgentHealthSummary, msg, 256)

	assert.Equal(t, []string{"cpu", "proc"}, familyLabels(samples),
		"only the agreed vocabulary is written")
	assertOneUnknownFamilyDrop(t, m)
}

func TestHealthSummaryOfJunkFamiliesWritesNoFamilySeries(t *testing.T) {
	families := make([]protocol.FamilyAnomalyRate, 0, 1000)
	for i := range 1000 {
		families = append(families, protocol.FamilyAnomalyRate{
			Family: fmt.Sprintf("invented.family.%d", i),
			Rate:   0.5,
		})
	}
	samples, m := familyIngest(t, (*AgentConn).handleAgentHealthSummary, healthSummaryOf(families), 4096)

	assert.Empty(t, familyLabels(samples), "no invented family becomes a series")
	assertOneUnknownFamilyDrop(t, m)
}

func TestHealthWindowResponseDropsUnlistedFamilies(t *testing.T) {
	msg := &protocol.ControlMessage{
		Type: protocol.MsgHealthWindowResponse,
		TS:   time.Now().Unix(),
		Summaries: []protocol.HealthSummary{{
			TS:              time.Now().Unix(),
			NodeAnomalyRate: 0.125,
			SamplerVersion:  "sysinfo-k2",
			PerFamilyRates: []protocol.FamilyAnomalyRate{
				{Family: "mem", Rate: 0.25},
				{Family: "gpu", Rate: 0.5},
			},
		}},
	}
	samples, m := familyIngest(t, (*AgentConn).handleHealthWindowResponse, msg, 256)

	assert.Equal(t, []string{"mem"}, familyLabels(samples),
		"only the agreed vocabulary is written")
	assertOneUnknownFamilyDrop(t, m)
}
