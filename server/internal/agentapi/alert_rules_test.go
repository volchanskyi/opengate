package agentapi

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestStaticAlertRuleProvider_TenantScopedWithDefault(t *testing.T) {
	orgA := uuid.New()
	orgB := uuid.New()
	ruleA := protocol.ThresholdRule{ID: "orgA-only", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 50, Clear: 40}
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{
		orgA: {ruleA},
	})

	got := provider.RulesFor(orgA)
	require.Len(t, got, 1)
	assert.Equal(t, "orgA-only", got[0].ID)

	// Org B has no override → the minimal default set, and never org A's rule.
	def := provider.RulesFor(orgB)
	assert.Equal(t, DefaultAlertRules(), def)
	for _, r := range def {
		assert.NotEqual(t, "orgA-only", r.ID, "org A's rule must never reach org B")
	}
}

func TestStaticAlertRuleProvider_ReturnsDefensiveCopy(t *testing.T) {
	org := uuid.New()
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{
		org: {{ID: "x", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 1, Clear: 0}},
	})
	got := provider.RulesFor(org)
	got[0].ID = "mutated"
	assert.Equal(t, "x", provider.RulesFor(org)[0].ID, "provider must hand back a copy the caller cannot mutate")
}

func TestAgentConn_PushAlertRules_ScopedToAuthoritativeOrg(t *testing.T) {
	orgA := uuid.New()
	orgB := uuid.New()
	ruleA := protocol.ThresholdRule{ID: "orgA-only", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 50, Clear: 40, SustainSecs: 10}
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{orgA: {ruleA}})

	// An agent authenticated as org A receives exactly org A's rule.
	acA := &AgentConn{OrgID: orgA, codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts}}
	var bufA bytes.Buffer
	acA.stream = &bufA
	require.NoError(t, acA.pushAlertRules(context.Background()))
	msgA := readReply(t, acA, &bufA)
	assert.Equal(t, protocol.MsgPushAlertRules, msgA.Type)
	require.Len(t, msgA.AlertRules, 1)
	assert.Equal(t, "orgA-only", msgA.AlertRules[0].ID)

	// An agent authenticated as org B receives the default set — never org A's rule.
	acB := &AgentConn{OrgID: orgB, codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts}}
	var bufB bytes.Buffer
	acB.stream = &bufB
	require.NoError(t, acB.pushAlertRules(context.Background()))
	msgB := readReply(t, acB, &bufB)
	require.NotEmpty(t, msgB.AlertRules)
	for _, r := range msgB.AlertRules {
		assert.NotEqual(t, "orgA-only", r.ID, "org A's rule must never reach org B")
	}
}

func TestAgentConn_PushAlertRules_RequiresCapability(t *testing.T) {
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), nil)
	ac := &AgentConn{OrgID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider}
	var buf bytes.Buffer
	ac.stream = &buf

	err := ac.pushAlertRules(context.Background())
	require.Error(t, err)
	assert.True(t, IsCapabilityError(err))
	assert.Zero(t, buf.Len(), "nothing may be written to an agent that did not advertise ThresholdAlerts")
}

func TestAgentConn_PushAlertRules_NilProviderNoOp(t *testing.T) {
	ac := &AgentConn{OrgID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger()}
	var buf bytes.Buffer
	ac.stream = &buf
	require.NoError(t, ac.pushAlertRules(context.Background()))
	assert.Zero(t, buf.Len())
}

func TestAgentConn_HandleAgentHealthSummary_IngestsBreachesOnly(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer

	// A breach-only summary carries no sampler computation: it must ingest the
	// breach series and MUST NOT write a bogus zero anomaly-rate sample.
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary,
		TS:   time.Now().Unix(),
		Breaches: []protocol.AlertBreach{
			{RuleID: "disk-critical", Metric: "disk.used", Value: 96.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 1)
	assert.Equal(t, "opengate_edge_alert_breach", call.samples[0].Name)
	assert.Equal(t, "disk-critical", call.samples[0].Labels["rule"])
	assert.Equal(t, "disk.used", call.samples[0].Labels["metric"])
	assert.InEpsilon(t, 96.0, call.samples[0].Value, 0.0001)
}

func TestAgentConn_HandleAgentHealthSummary_IngestsAnomalyAndBreaches(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer

	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type:            protocol.MsgAgentHealthSummary,
		TS:              time.Now().Unix(),
		NodeAnomalyRate: 0.3,
		SamplerVersion:  "sysinfo-k2",
		Breaches: []protocol.AlertBreach{
			{RuleID: "cpu-saturated", Metric: "cpu.total", Value: 97.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 2)
	assert.Equal(t, "opengate_edge_node_anomaly_rate", call.samples[0].Name)
	assert.Equal(t, "opengate_edge_alert_breach", call.samples[1].Name)
	assert.Equal(t, "cpu-saturated", call.samples[1].Labels["rule"])
}

func TestAgentConn_HandleAgentHealthSummary_DropsUnknownBreachMetric(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer

	// An agent-supplied breach whose metric is outside the known vocabulary is
	// dropped so a rogue agent cannot drive unbounded label cardinality.
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary,
		TS:   time.Now().Unix(),
		Breaches: []protocol.AlertBreach{
			{RuleID: "evil", Metric: "../../etc/passwd", Value: 1.0},
			{RuleID: "disk-critical", Metric: "disk.used", Value: 96.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 1)
	assert.Equal(t, "disk.used", call.samples[0].Labels["metric"])
}

func TestSanitizeAlertRuleID(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", maxAlertRuleIDLen+10)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"embedded space redacted", "rule id", "[redacted]"},
		{"embedded newline redacted", "rule\nid", "[redacted]"},
		{"plain id kept", "disk-critical", "disk-critical"},
		{"trimmed", "  cpu-high  ", "cpu-high"},
		{"overlong rune-capped", long, strings.Repeat("a", maxAlertRuleIDLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeAlertRuleID(tt.in))
		})
	}
}
