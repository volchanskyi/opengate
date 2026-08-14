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

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/inventory"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// testDim is the dimension name every accounting fixture reports under.
const testDim = "cpu.total"

// countedIngestByIdent pins the control types whose handlers increment
// opengate_edge_telemetry_ingested_total, keyed by the protocol identifier the
// source uses. Every entry needs at least one row in accountingCases; every
// counted-ingest call site in the package must appear here. Both directions are
// asserted by TestCountedIngestTypesMatchDispatch, so a new telemetry message
// type cannot join the pipeline without joining the ledger.
var countedIngestByIdent = map[string]protocol.ControlMessageType{
	"MsgAgentHealthSummary":   protocol.MsgAgentHealthSummary,
	"MsgAgentMetricWindow":    protocol.MsgAgentMetricWindow,
	"MsgProcessReport":        protocol.MsgProcessReport,
	"MsgDiscoveryReport":      protocol.MsgDiscoveryReport,
	"MsgHealthWindowResponse": protocol.MsgHealthWindowResponse,
}

// dispatchNonIngestIdents pins the control types handleControl dispatches that
// carry no counted telemetry: session/update signalling, hardware and log
// responses, and the backfill path (which keeps its own 90 d retention bound and
// never touches the ingest counter).
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
	// An alert is validated and admitted but not yet stored, so it produces no
	// state for the ingest ledger to balance against. It joins the counted set
	// when the alert store lands beside it.
	"MsgAgentAlert",
}

// preIngestDropReasons are the bounds applied before the ingest counter fires.
// A message dropped for one of these was never ingested, so it belongs on
// neither side of the ingest ledger.
var preIngestDropReasons = map[string]bool{
	"payload_too_large":           true,
	"interval_floor":              true,
	"discovery_payload_too_large": true,
	"discovery_interval_floor":    true,
	"tombstoned":                  true,
}

// accountingSinks counts every persist a driven message produced, across the
// three stores telemetry can land in.
type accountingSinks struct {
	writes  atomic.Int64
	failErr error
}

func (s *accountingSinks) WriteSamples(context.Context, uuid.UUID, uuid.UUID, []telemetry.Sample) error {
	if s.failErr != nil {
		return s.failErr
	}
	s.writes.Add(1)
	return nil
}

func (s *accountingSinks) UpsertReport(context.Context, uuid.UUID, time.Time, []telemetry.ProcessSample) error {
	if s.failErr != nil {
		return s.failErr
	}
	s.writes.Add(1)
	return nil
}

func (s *accountingSinks) ListLatest(context.Context, uuid.UUID, int) ([]telemetry.ProcessSample, error) {
	return nil, nil
}

func (s *accountingSinks) Replace(context.Context, uuid.UUID, time.Time, []inventory.Component) error {
	if s.failErr != nil {
		return s.failErr
	}
	s.writes.Add(1)
	return nil
}

func (s *accountingSinks) ListForDevice(context.Context, uuid.UUID, int) ([]inventory.Component, error) {
	return nil, nil
}

// accountingCase drives one or more control messages through a real AgentConn
// and pins the whole ledger they produce: how many were counted as ingested, how
// many produced at least one persist, how many persists happened, and which
// typed drops fired.
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

// accountingCases covers every branch of every counted-ingest control type:
// the persisting branch, the empty-payload branch that used to return silently,
// the two admission bounds, and the three persist-path failures that discard a
// whole coalesced batch.
func accountingCases(now int64) []accountingCase {
	pkg := protocol.DiscoveredPackage{Name: "openssl", Version: "3.0.13"}
	dim := protocol.MetricDim{Name: testDim, Avg: 12.5}
	summary := protocol.HealthSummary{TS: now, NodeAnomalyRate: 0.4, SamplerVersion: "s1"}
	entry := protocol.ProcessReportEntry{Rank: 1, Basename: "postgres", PID: 222, CPU: 12.5, Mem: 3.25}

	return []accountingCase{
		{
			name:      "metric window persists its dims",
			msgs:      []*protocol.ControlMessage{metricWindowMsg(now, dim)},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		{
			name:     "metric window with no dims is a typed drop",
			msgs:     []*protocol.ControlMessage{metricWindowMsg(now)},
			ingested: 1,
			drops:    map[string]int{"empty_dims": 1},
		},
		{
			name:  "metric window over the payload cap never reaches the ingest counter",
			msgs:  []*protocol.ControlMessage{metricWindowMsg(now, dim)},
			pad:   maxTelemetryPayloadBytes + 1,
			drops: map[string]int{"payload_too_large": 1},
		},
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
		{
			name:     "health summary with nothing to record is a typed drop",
			msgs:     []*protocol.ControlMessage{{Type: protocol.MsgAgentHealthSummary, TS: now}},
			ingested: 1,
			drops:    map[string]int{"empty_summary": 1},
		},
		{
			// A calm machine's summary says what every rule is doing on it and
			// nothing else. That is state the server now holds, so the message
			// belongs on the produced-state side of the ledger rather than being
			// filed as a discard it plainly is not.
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
		{
			name: "health summary over the payload cap never reaches the ingest counter",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgAgentHealthSummary, TS: now, SamplerVersion: "s1",
			}},
			pad:   maxTelemetryPayloadBytes + 1,
			drops: map[string]int{"payload_too_large": 1},
		},
		{
			name: "process report persists rows and rank numerics",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgProcessReport, TS: now, TopN: []protocol.ProcessReportEntry{entry},
			}},
			ingested:  1,
			persisted: 1,
			writes:    2,
		},
		{
			name:     "process report with no processes is a typed drop",
			msgs:     []*protocol.ControlMessage{{Type: protocol.MsgProcessReport, TS: now}},
			ingested: 1,
			drops:    map[string]int{"empty_processes": 1},
		},
		{
			name: "process report over the payload cap never reaches the ingest counter",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgProcessReport, TS: now, TopN: []protocol.ProcessReportEntry{entry},
			}},
			pad:   maxTelemetryPayloadBytes + 1,
			drops: map[string]int{"payload_too_large": 1},
		},
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
		{
			name:     "health window response with no summaries is a typed drop",
			msgs:     []*protocol.ControlMessage{{Type: protocol.MsgHealthWindowResponse, TS: now}},
			ingested: 1,
			drops:    map[string]int{"empty_summaries": 1},
		},
		{
			name: "health window response over the payload cap never reaches the ingest counter",
			msgs: []*protocol.ControlMessage{{
				Type: protocol.MsgHealthWindowResponse, TS: now,
				Summaries: []protocol.HealthSummary{summary},
			}},
			pad:   maxTelemetryPayloadBytes + 1,
			drops: map[string]int{"payload_too_large": 1},
		},
		{
			name:      "discovery report persists its footprint",
			msgs:      []*protocol.ControlMessage{discoveryMsg(now, pkg)},
			ingested:  1,
			persisted: 1,
			writes:    1,
		},
		{
			name:     "discovery report with no components is a typed drop",
			msgs:     []*protocol.ControlMessage{discoveryMsg(now)},
			ingested: 1,
			drops:    map[string]int{"empty_discovery": 1},
		},
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
			name: "a coalesced batch flushed without a tenant drops every message it carried",
			msgs: []*protocol.ControlMessage{
				metricWindowMsg(now, dim),
				metricWindowMsg(now+minTelemetryIntervalSeconds, dim),
			},
			flushWithoutTenant: true,
			ingested:           2,
			drops:              map[string]int{"tenant_missing": 2},
		},
		{
			name: "a coalesced batch shed by full persist slots drops every message it carried",
			msgs: []*protocol.ControlMessage{
				metricWindowMsg(now, dim),
				metricWindowMsg(now+minTelemetryIntervalSeconds, dim),
			},
			fillSlots: true,
			ingested:  2,
			drops:     map[string]int{"persist_slots_full": 2},
		},
		{
			name: "a coalesced batch whose write fails drops every message it carried",
			msgs: []*protocol.ControlMessage{
				metricWindowMsg(now, dim),
				metricWindowMsg(now+minTelemetryIntervalSeconds, dim),
			},
			failWrites: true,
			ingested:   2,
			drops:      map[string]int{"persist_failed": 2},
		},
	}
}

// errAccountingWrite is the sentinel the failing-store cases return.
var errAccountingWrite = errors.New("accounting write failed")

// runAccountingCase drives one case through a real AgentConn on a fresh
// connection sharing the caller's metrics registry, and returns the persists it
// produced. Cases run on their own connection so the interval floor of one never
// leaks into the next, while the shared registry keeps the ledger cumulative.
func runAccountingCase(t *testing.T, m *appmetrics.Metrics, tc accountingCase) int64 {
	t.Helper()
	sinks := &accountingSinks{}
	if tc.failWrites {
		sinks.failErr = errAccountingWrite
	}
	var buf bytes.Buffer
	tenant := uuid.New()
	ac := &AgentConn{
		DeviceID:     uuid.New(),
		TenantID:     tenant,
		stream:       &buf,
		codec:        &protocol.Codec{},
		telemetry:    sinks,
		processes:    sinks,
		inventory:    sinks,
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

// TestTelemetryAccountingInvariant is the durable fix for the silent-loss class:
// every counted-ingest branch either persists or files exactly one typed drop
// per message, and the two sides of the ledger balance.
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
	// The invariant: everything counted as ingested either produced state — a
	// persisted write, or a rule-coverage report the server now holds — or was
	// filed under exactly one drop reason. Nothing vanishes in between.
	assert.Equal(t, wantIngested, wantPersisted+wantPostIngestDrops,
		"the case table itself must balance")
	assert.InDelta(t, float64(wantPersisted+wantPostIngestDrops), gotIngested, 0,
		"ingested must equal persisted plus post-ingest drops")
}
