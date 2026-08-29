package agentapi

import (
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

// familyLabels is the family label of every sample a write carried, in order.
func familyLabels(samples []telemetry.Sample) []string {
	names := make([]string, 0, len(samples))
	for _, s := range samples {
		if family, ok := s.Labels["family"]; ok {
			names = append(names, family)
		}
	}
	return names
}

// A summary carrying an unlisted family persists the listed ones and nothing
// else, and says so through the drop counter rather than silently.
func TestHealthSummaryDropsUnlistedFamilies(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	msg := &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		SamplerVersion:  "sysinfo-k2",
		NodeAnomalyRate: 0.125,
		PerFamilyRates: []protocol.FamilyAnomalyRate{
			{Family: "cpu", Rate: 0.25},
			{Family: "process", Rate: 0.5},
			{Family: "proc", Rate: 0.75},
		},
	}
	require.NoError(t, ac.handleAgentHealthSummary(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))

	call := <-writer.calls
	assert.Equal(t, []string{"cpu", "proc"}, familyLabels(call.samples),
		"only the agreed vocabulary is written")
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_family")), 0)
}

// The cardinality argument in one test: a summary of a thousand invented
// families creates no family series at all, so a misbehaving agent cannot
// enlarge the central store. Without the allowlist every one would be a label.
func TestHealthSummaryOfJunkFamiliesWritesNoFamilySeries(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

	families := make([]protocol.FamilyAnomalyRate, 0, 1000)
	for i := range 1000 {
		families = append(families, protocol.FamilyAnomalyRate{
			Family: fmt.Sprintf("invented.family.%d", i),
			Rate:   0.5,
		})
	}
	msg := &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		SamplerVersion:  "sysinfo-k2",
		NodeAnomalyRate: 0.125,
		PerFamilyRates:  families,
	}
	require.NoError(t, ac.handleAgentHealthSummary(tenantCtx(tenant), msg, 4096))
	ac.flushTelemetry(tenantCtx(tenant))

	call := <-writer.calls
	assert.Empty(t, familyLabels(call.samples), "no invented family becomes a series")
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_family")), 0)
}

// The read-back path copies the same label from the same untrusted string, so
// it is filtered by the same vocabulary.
func TestHealthWindowResponseDropsUnlistedFamilies(t *testing.T) {
	tenant := uuid.New()
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, _ := ingestConn(t, tenant, writer, true)
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	ac.metrics = m

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
	require.NoError(t, ac.handleHealthWindowResponse(tenantCtx(tenant), msg, 256))
	ac.flushTelemetry(tenantCtx(tenant))

	call := <-writer.calls
	assert.Equal(t, []string{"mem"}, familyLabels(call.samples),
		"only the agreed vocabulary is written")
	assert.InDelta(t, 1,
		testutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues("unknown_family")), 0)
}
