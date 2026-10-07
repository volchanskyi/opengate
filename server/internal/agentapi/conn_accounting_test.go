package agentapi

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

const testDim = "cpu.total"

var countedIngestByIdent = map[string]protocol.ControlMessageType{
	"MsgAgentHealthSummary":   protocol.MsgAgentHealthSummary,
	"MsgAgentMetricWindow":    protocol.MsgAgentMetricWindow,
	"MsgProcessReport":        protocol.MsgProcessReport,
	"MsgDiscoveryReport":      protocol.MsgDiscoveryReport,
	"MsgHealthWindowResponse": protocol.MsgHealthWindowResponse,
	"MsgAgentAlert":           protocol.MsgAgentAlert,
}

var dispatchNonIngestIdents = []string{
	"MsgAgentRegister",
	"MsgAgentHeartbeat",
	"MsgSessionAccept",
	"MsgSessionReject",
	"MsgAgentUpdateAck",
	"MsgHardwareReport",
	"MsgHardwareReportError",
	"MsgDeviceLogsResponse",
	"MsgDeviceLogsError",
	"MsgRequestBackfillSlot",
	"MsgMetricBackfillBatch",
	"MsgLocalHistoryResponse",
	"MsgMaintenanceApplied",
}

// preIngestDropReasons are the bounds applied before the ingest counter fires, so their drops
// sit on neither side of the ledger.
var preIngestDropReasons = map[string]bool{
	"payload_too_large":           true,
	"interval_floor":              true,
	"discovery_payload_too_large": true,
	"discovery_interval_floor":    true,
	"alert_payload_too_large":     true,
	"tombstoned":                  true,
}

type accountingSinks struct {
	writes  atomic.Int64
	failErr error
}

func (s *accountingSinks) accept() error {
	if s.failErr != nil {
		return s.failErr
	}
	s.writes.Add(1)
	return nil
}

func (s *accountingSinks) WriteSamples(context.Context, uuid.UUID, uuid.UUID, []telemetry.Sample) error {
	return s.accept()
}

func (s *accountingSinks) UpsertReport(context.Context, uuid.UUID, time.Time, []telemetry.ProcessSample) error {
	return s.accept()
}

func (s *accountingSinks) ListLatest(context.Context, uuid.UUID, int) ([]telemetry.ProcessSample, error) {
	return nil, nil
}

func (s *accountingSinks) Replace(context.Context, uuid.UUID, time.Time, []inventory.Component) error {
	return s.accept()
}

func (s *accountingSinks) ListForDevice(context.Context, uuid.UUID, int) ([]inventory.Component, error) {
	return nil, nil
}

func (s *accountingSinks) Record(context.Context, alerts.Alert, alerts.Grouping) (alerts.Outcome, error) {
	if err := s.accept(); err != nil {
		return "", err
	}
	return alerts.Stored, nil
}

type accountingCase struct {
	name string
	msgs []*protocol.ControlMessage
	// pad inflates the last message so it breaches a payload cap.
	pad int
	// fillSlots saturates the persist slots before the flush.
	fillSlots bool
	// failWrites makes every store return an error.
	failWrites bool
	// flushWithoutTenant flushes on a context carrying no tenant.
	flushWithoutTenant bool

	ingested  int
	persisted int
	writes    int
	drops     map[string]int
}

func metricWindowMsg(ts int64, dims ...protocol.MetricDim) *protocol.ControlMessage {
	return &protocol.ControlMessage{Type: protocol.MsgAgentMetricWindow, TS: ts, Dims: dims}
}

func discoveryMsg(ts int64, packages ...protocol.DiscoveredPackage) *protocol.ControlMessage {
	return &protocol.ControlMessage{Type: protocol.MsgDiscoveryReport, TS: ts, Packages: packages}
}

// alertMsg builds a well-formed alert stamped inside the live clock window.
func alertMsg(ts int64, severityValue protocol.AlertSeverity) *protocol.ControlMessage {
	return &protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.NewString(),
		RuleID:        "disk-critical",
		RuleVersion:   1,
		Severity:      &severityValue,
		Metric:        "disk.used_percent",
		WindowStartTS: ts - 300,
		WindowEndTS:   ts,
		ObservedTS:    ts,
	}
}

func typedDrop(kind string, msg *protocol.ControlMessage, reason string) accountingCase {
	return accountingCase{
		name:     kind + " is a typed drop",
		msgs:     []*protocol.ControlMessage{msg},
		ingested: 1,
		drops:    map[string]int{reason: 1},
	}
}

func overCap(kind string, msg *protocol.ControlMessage) accountingCase {
	return accountingCase{
		name:  kind + " over the payload cap never reaches the ingest counter",
		msgs:  []*protocol.ControlMessage{msg},
		pad:   maxTelemetryPayloadBytes + 1,
		drops: map[string]int{"payload_too_large": 1},
	}
}

func accountingCases(now int64) []accountingCase {
	pkg := protocol.DiscoveredPackage{Name: "openssl", Version: "3.0.13"}
	dim := protocol.MetricDim{Name: testDim, Avg: 12.5}
	summary := protocol.HealthSummary{TS: now, NodeAnomalyRate: 0.4, SamplerVersion: "s1"}
	entry := protocol.ProcessReportEntry{Rank: 1, Basename: "postgres", PID: 222, CPU: 12.5, Mem: 3.25}
	coalesced := []*protocol.ControlMessage{
		metricWindowMsg(now, dim),
		metricWindowMsg(now+minTelemetryIntervalSeconds, dim),
	}

	return []accountingCase{
		{
			name:      "metric window persists its dims",
			msgs:      []*protocol.ControlMessage{metricWindowMsg(now, dim)},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		typedDrop("metric window with no dims", metricWindowMsg(now), "empty_dims"),
		overCap("metric window", metricWindowMsg(now, dim)),
		{
			name: "metric window inside the interval floor is dropped",
			msgs: []*protocol.ControlMessage{
				metricWindowMsg(now, dim),
				metricWindowMsg(now+1, dim),
			},
			ingested:  1,
			persisted: 1,
			writes:    1,
			drops:     map[string]int{"interval_floor": 1},
		},
		{
			name: "health summary persists its sampler result",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgAgentHealthSummary, TS: now,
				NodeAnomalyRate: 0.25, SamplerVersion: "s1",
			}},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		typedDrop("health summary with nothing to record",
			&protocol.ControlMessage{Type: protocol.MsgAgentHealthSummary, TS: now}, "empty_summary"),
		{
			// A coverage-only summary is state the server holds, so it counts as produced.
			name: "health summary carrying only rule coverage produces state, not a drop",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgAgentHealthSummary, TS: now,
				RuleCoverage: []protocol.RuleCoverage{
					{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
				},
			}},
			ingested:  1,
			persisted: 1,
		},
		overCap("health summary", &protocol.ControlMessage{
			Type: protocol.MsgAgentHealthSummary, TS: now, SamplerVersion: "s1",
		}),
		{
			name: "process report persists rows and rank numerics",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgProcessReport, TS: now, TopN: []protocol.ProcessReportEntry{entry},
			}},
			ingested:  1,
			persisted: 1,
			writes:    2,
		},
		typedDrop("process report with no processes",
			&protocol.ControlMessage{Type: protocol.MsgProcessReport, TS: now}, "empty_processes"),
		overCap("process report", &protocol.ControlMessage{
			Type: protocol.MsgProcessReport, TS: now, TopN: []protocol.ProcessReportEntry{entry},
		}),
		{
			name: "health window response persists its summaries",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgHealthWindowResponse, TS: now,
				Summaries: []protocol.HealthSummary{summary},
			}},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		typedDrop("health window response with no summaries",
			&protocol.ControlMessage{Type: protocol.MsgHealthWindowResponse, TS: now}, "empty_summaries"),
		overCap("health window response", &protocol.ControlMessage{
			Type: protocol.MsgHealthWindowResponse, TS: now,
			Summaries: []protocol.HealthSummary{summary},
		}),
		{
			name:      "discovery report persists its footprint",
			msgs:      []*protocol.ControlMessage{discoveryMsg(now, pkg)},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		typedDrop("discovery report with no components", discoveryMsg(now), "empty_discovery"),
		{
			name:  "discovery report over the payload cap never reaches the ingest counter",
			msgs:  []*protocol.ControlMessage{discoveryMsg(now, pkg)},
			pad:   maxDiscoveryPayloadBytes + 1,
			drops: map[string]int{"discovery_payload_too_large": 1},
		},
		{
			name: "discovery report inside the interval floor is dropped",
			msgs: []*protocol.ControlMessage{
				discoveryMsg(now, pkg),
				discoveryMsg(now+1, pkg),
			},
			ingested:  1,
			persisted: 1,
			writes:    1,
			drops:     map[string]int{"discovery_interval_floor": 1},
		},
		{
			name:      "an alert is stored against the customer that raised it",
			msgs:      []*protocol.ControlMessage{alertMsg(now, protocol.AlertSeverityCritical)},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		{
			// Alert refusals are content checks after the ingest counter, so the ledger balances.
			name:     "an alert whose severity is outside the set is a typed drop",
			msgs:     []*protocol.ControlMessage{alertMsg(now, protocol.AlertSeverity("Catastrophic"))},
			ingested: 1,
			drops:    map[string]int{alertDropSeverityUnknown: 1},
		},
		{
			name:               "a coalesced batch flushed without a tenant drops every message it carried",
			msgs:               coalesced,
			flushWithoutTenant: true,
			ingested:           2,
			drops:              map[string]int{"tenant_missing": 2},
		},
		{
			name:      "a coalesced batch shed by full persist slots drops every message it carried",
			msgs:      coalesced,
			fillSlots: true,
			ingested:  2,
			drops:     map[string]int{"persist_slots_full": 2},
		},
		{
			name:       "a coalesced batch whose write fails drops every message it carried",
			msgs:       coalesced,
			failWrites: true,
			ingested:   2,
			drops:      map[string]int{"persist_failed": 2},
		},
		{
			// The endpoint drops its copy once it believes an alert landed, so a store failure
			// counts as a loss.
			name:       "an alert the store cannot take is counted as lost",
			msgs:       []*protocol.ControlMessage{alertMsg(now, protocol.AlertSeverityWarning)},
			failWrites: true,
			ingested:   1,
			drops:      map[string]int{"persist_failed": 1},
		},
	}
}

var errAccountingWrite = errors.New("accounting write failed")

// runAccountingCase runs a case on a fresh connection that shares the caller's metrics registry,
// so the interval floor never leaks between cases while the ledger stays cumulative.
func runAccountingCase(t *testing.T, m *appmetrics.Metrics, tc accountingCase) int64 {
	t.Helper()
	sinks := &accountingSinks{}
	if tc.failWrites {
		sinks.failErr = errAccountingWrite
	}
	var buf bytes.Buffer
	tenant := uuid.New()
	deviceID := uuid.New()
	ac := &AgentConn{
		DeviceID:  deviceID,
		TenantID:  tenant,
		stream:    &buf,
		codec:     &protocol.Codec{},
		telemetry: sinks,
		processes: sinks,
		inventory: sinks,
		// An alert is filed against the machine's customer, so that rung must resolve.
		settings: fixedReader{scope: settings.Scope{
			DeviceID: deviceID, OrganizationID: uuid.New(), TenantID: tenant,
		}},
		alertStore:   sinks,
		metrics:      m,
		coverage:     NewRuleCoverageStore(),
		Capabilities: []protocol.AgentCapability{protocol.CapDiscovery},
		logger:       testLogger(),
	}
	if tc.fillSlots {
		ac.telemetrySlots = make(chan struct{}, telemetryConcurrentWrites)
		for range telemetryConcurrentWrites {
			ac.telemetrySlots <- struct{}{}
		}
	}

	for i, msg := range tc.msgs {
		padded := *msg
		if tc.pad > 0 && i == len(tc.msgs)-1 {
			padded.Reason = strings.Repeat("x", tc.pad)
		}
		writeControlMsg(t, ac.codec, &buf, &padded)
	}
	ctx := dbtx.WithTenant(context.Background(), tenant, false)
	for range tc.msgs {
		require.NoError(t, ac.handleControl(ctx))
	}
	flushCtx := ctx
	if tc.flushWithoutTenant {
		flushCtx = context.Background()
	}
	ac.flushTelemetry(flushCtx)

	require.Eventually(t, func() bool {
		return sinks.writes.Load() >= int64(tc.writes)
	}, 2*time.Second, 5*time.Millisecond, "expected %d persists", tc.writes)
	return sinks.writes.Load()
}

func TestTelemetryAccountingInvariant(t *testing.T) {
	now := time.Now().Unix()
	cases := accountingCases(now)

	var wantIngested, wantPersisted, wantPostIngestDrops int
	wantDrops := map[string]int{}
	m := appmetrics.NewMetrics(prometheus.NewRegistry())

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := map[string]float64{}
			for reason := range tc.drops {
				before[reason] = promtestutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues(reason))
			}
			gotWrites := runAccountingCase(t, m, tc)
			assert.Equal(t, int64(tc.writes), gotWrites, "persist count")

			for reason, want := range tc.drops {
				assert.Eventuallyf(t, func() bool {
					got := promtestutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues(reason))
					return got-before[reason] == float64(want)
				}, 2*time.Second, 5*time.Millisecond, "drop reason %s", reason)
			}
		})

		wantIngested += tc.ingested
		wantPersisted += tc.persisted
		for reason, n := range tc.drops {
			wantDrops[reason] += n
			if !preIngestDropReasons[reason] {
				wantPostIngestDrops += n
			}
		}
	}

	var gotIngested float64
	for _, msgType := range countedIngestByIdent {
		gotIngested += promtestutil.ToFloat64(
			m.EdgeTelemetryIngestedTotal.WithLabelValues(string(msgType)))
	}
	for reason, want := range wantDrops {
		got := promtestutil.ToFloat64(m.EdgeTelemetryDropsTotal.WithLabelValues(reason))
		assert.InDelta(t, want, got, 0, "cumulative drops for %s", reason)
	}

	assert.InDelta(t, wantIngested, gotIngested, 0, "cumulative ingested")
	// Every ingested message either produced state or was filed under exactly one drop reason.
	assert.Equal(t, wantIngested, wantPersisted+wantPostIngestDrops,
		"the case table itself must balance")
	assert.InDelta(t, float64(wantPersisted+wantPostIngestDrops), gotIngested, 0,
		"ingested must equal persisted plus post-ingest drops")
}
