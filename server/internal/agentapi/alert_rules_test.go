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
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// inTenant is the ladder for a machine whose only known rung is its tenant,
// which is what a static provider reads.
func inTenant(tenantID uuid.UUID) settings.Scope {
	return settings.Scope{DeviceID: uuid.New(), OrganizationID: uuid.New(), TenantID: tenantID}
}

func TestStaticAlertRuleProvider_TenantScopedWithDefault(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	ruleA := protocol.ThresholdRule{ID: "tenantA-only", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 50, Clear: 40}
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{
		tenantA: {ruleA},
	})

	got := provider.RulesFor(inTenant(tenantA))
	require.Len(t, got, 1)
	assert.Equal(t, "tenantA-only", got[0].ID)

	// Tenant B has no override → the minimal default set, and never tenant A's rule.
	def := provider.RulesFor(inTenant(tenantB))
	assert.Equal(t, DefaultAlertRules(), def)
	for _, r := range def {
		assert.NotEqual(t, "tenantA-only", r.ID, "tenant A's rule must never reach tenant B")
	}
}

func TestStaticAlertRuleProvider_ReturnsDefensiveCopy(t *testing.T) {
	tenant := uuid.New()
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{
		tenant: {{ID: "x", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 1, Clear: 0}},
	})
	got := provider.RulesFor(inTenant(tenant))
	got[0].ID = "mutated"
	assert.Equal(t, "x", provider.RulesFor(inTenant(tenant))[0].ID, "provider must hand back a copy the caller cannot mutate")
}

func TestAgentConn_PushAlertRules_ScopedToAuthoritativeTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	ruleA := protocol.ThresholdRule{ID: "tenantA-only", Metric: "cpu.total", Comparator: protocol.AlertComparatorGt, Threshold: 50, Clear: 40, SustainSecs: 10}
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), map[uuid.UUID][]protocol.ThresholdRule{tenantA: {ruleA}})

	// An agent authenticated as tenant A receives exactly tenant A's rule.
	acA := &AgentConn{TenantID: tenantA, codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts}}
	var bufA bytes.Buffer
	acA.stream = &bufA
	require.NoError(t, acA.pushAlertRules(context.Background()))
	msgA := readReply(t, acA, &bufA)
	assert.Equal(t, protocol.MsgPushAlertRules, msgA.Type)
	require.Len(t, msgA.AlertRules, 1)
	assert.Equal(t, "tenantA-only", msgA.AlertRules[0].ID)

	// An agent authenticated as tenant B receives the default set — never tenant A's rule.
	acB := &AgentConn{TenantID: tenantB, codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider,
		Capabilities: []protocol.AgentCapability{protocol.CapThresholdAlerts}}
	var bufB bytes.Buffer
	acB.stream = &bufB
	require.NoError(t, acB.pushAlertRules(context.Background()))
	msgB := readReply(t, acB, &bufB)
	require.NotEmpty(t, msgB.AlertRules)
	for _, r := range msgB.AlertRules {
		assert.NotEqual(t, "tenantA-only", r.ID, "tenant A's rule must never reach tenant B")
	}
}

func TestAgentConn_PushAlertRules_RequiresCapability(t *testing.T) {
	provider := NewStaticAlertRuleProvider(DefaultAlertRules(), nil)
	ac := &AgentConn{TenantID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger(), alertRules: provider}
	var buf bytes.Buffer
	ac.stream = &buf

	err := ac.pushAlertRules(context.Background())
	require.Error(t, err)
	assert.True(t, IsCapabilityError(err))
	assert.Zero(t, buf.Len(), "nothing may be written to an agent that did not advertise ThresholdAlerts")
}

func TestAgentConn_PushAlertRules_NilProviderNoOp(t *testing.T) {
	ac := &AgentConn{TenantID: uuid.New(), codec: &protocol.Codec{}, logger: testLogger()}
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
			{RuleID: "disk-critical", Metric: "disk.used_percent", Value: 96.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
	ac.flushTelemetry(dbtx.WithDefaultTenant(context.Background(), false))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 1)
	assert.Equal(t, "opengate_edge_alert_breach", call.samples[0].Name)
	assert.Equal(t, "disk-critical", call.samples[0].Labels["rule"])
	assert.Equal(t, "disk.used_percent", call.samples[0].Labels["metric"])
	assert.InEpsilon(t, 96.0, call.samples[0].Value, 0.0001)
}

func TestAgentConn_HandleAgentHealthSummary_ResolvesLegacyBreachMetricNames(t *testing.T) {
	writer := &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.telemetry = writer

	// An agent that predates the rename reports the breach under the old name.
	// Central must record it under the canonical one, or the same rule on the
	// same reading occupies two series and neither tells the whole story.
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAgentHealthSummary,
		TS:   time.Now().Unix(),
		Breaches: []protocol.AlertBreach{
			{RuleID: "disk-critical", Metric: "disk.used", Value: 96.0},
			{RuleID: "memory-pressure", Metric: "mem.used", Value: 97.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
	ac.flushTelemetry(dbtx.WithDefaultTenant(context.Background(), false))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 2)
	assert.Equal(t, "disk.used_percent", call.samples[0].Labels["metric"])
	assert.Equal(t, "mem.used_percent", call.samples[1].Labels["metric"])
}

func TestDefaultAlertRules_UseCanonicalMetricNames(t *testing.T) {
	t.Parallel()
	for _, r := range DefaultAlertRules() {
		canonical, ok := protocol.CanonicalRuleMetric(r.Metric)
		require.True(t, ok, "%s watches %s, which is outside the rule vocabulary", r.ID, r.Metric)
		assert.Equal(t, canonical, r.Metric, "%s must be declared under the canonical name", r.ID)
		assert.True(t, isVitalDim(r.Metric), "%s watches a dimension central telemetry does not store", r.ID)
	}
}

func TestRuleVocabularyIsASubsetOfTheVitalsContract(t *testing.T) {
	t.Parallel()
	// A rule may only watch something the fleet agreed to collect. If the two
	// lists ever part company, a rule can fire on a reading nobody stores.
	for _, name := range protocol.RuleMetrics {
		assert.True(t, isVitalDim(name), "%s is in the rule vocabulary but not in the vitals contract", name)
	}
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
	ac.flushTelemetry(dbtx.WithDefaultTenant(context.Background(), false))

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
			{RuleID: "disk-critical", Metric: "disk.used_percent", Value: 96.0},
		},
	})
	require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))
	ac.flushTelemetry(dbtx.WithDefaultTenant(context.Background(), false))

	call := receiveTelemetryCall(t, writer.calls)
	require.Len(t, call.samples, 1)
	assert.Equal(t, "disk.used_percent", call.samples[0].Labels["metric"])
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
