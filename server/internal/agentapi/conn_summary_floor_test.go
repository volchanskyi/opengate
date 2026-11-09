package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// anomalySummary is the periodic summary a calm machine sends: a sampler result and its rates.
func anomalySummary(ts int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary, TS: ts,
		SamplerVersion: "s1", ModelVersion: "m1", NodeAnomalyRate: 0.02,
		PerFamilyRates: []protocol.FamilyAnomalyRate{{Family: "cpu", Rate: 0.01}},
	}
}

// breachSummary is the summary a machine sends while a rule is firing: breaches and no sampler.
func breachSummary(ts int64) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary, TS: ts,
		Breaches: []protocol.AlertBreach{{RuleID: "cpu-high", Metric: "cpu.total", Value: 97}},
	}
}

func TestAnAnomalySummaryAndABreachSummaryInOneSecondAreBothKept(t *testing.T) {
	ac, buf, writer := telemetryConn(t, 2)
	ctx := dbtx.WithDefaultTenant(context.Background(), false)
	now := time.Now().Unix()

	writeControlMsg(t, ac.codec, buf, anomalySummary(now))
	writeControlMsg(t, ac.codec, buf, breachSummary(now+1))
	require.NoError(t, ac.handleControl(ctx))
	require.NoError(t, ac.handleControl(ctx))
	ac.flushTelemetry(ctx)

	call := receiveTelemetryCall(t, writer.calls)
	names := make([]string, 0, len(call.samples))
	for _, s := range call.samples {
		names = append(names, s.Name)
	}
	assert.Contains(t, names, "opengate_edge_node_anomaly_rate")
	assert.Contains(t, names, "opengate_edge_alert_breach")
	assert.Zero(t, ac.DroppedTelemetryCount(), "each kind keeps its own ten-second floor")
}

func TestTwoSummariesOfOneKindInsideTheFloorKeepTheFirst(t *testing.T) {
	for _, c := range []struct {
		name    string
		summary func(int64) *protocol.ControlMessage
	}{
		{"breach", breachSummary},
		{"anomaly", anomalySummary},
	} {
		t.Run(c.name, func(t *testing.T) {
			ac, buf, writer := telemetryConn(t, 2)
			ctx := dbtx.WithDefaultTenant(context.Background(), false)
			now := time.Now().Unix()

			writeControlMsg(t, ac.codec, buf, c.summary(now))
			writeControlMsg(t, ac.codec, buf, c.summary(now+1))
			require.NoError(t, ac.handleControl(ctx))
			require.NoError(t, ac.handleControl(ctx))
			ac.flushTelemetry(ctx)

			receiveTelemetryCall(t, writer.calls)
			assert.Equal(t, uint64(1), ac.DroppedTelemetryCount())
		})
	}
}
