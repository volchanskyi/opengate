package agentapi

import (
	"bytes"
	"compress/flate"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/dbtx"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// alertFixture is a connection wired as production wires one, plus what a case reads back.
type alertFixture struct {
	conn    *AgentConn
	store   *recordingAlertStore
	metrics *appmetrics.Metrics
	scope   settings.Scope
	ctx     context.Context
}

func alertConn(t *testing.T) alertFixture {
	t.Helper()
	catalogue, err := rules.Embedded()
	require.NoError(t, err)
	scope := settings.Scope{
		DeviceID:       uuid.New(),
		SiteID:         uuid.New(),
		OrganizationID: uuid.New(),
		TenantID:       uuid.New(),
	}
	store := &recordingAlertStore{}
	m := appmetrics.NewMetrics(prometheus.NewRegistry())
	return alertFixture{
		conn: &AgentConn{
			DeviceID:    scope.DeviceID,
			SiteID:      scope.SiteID,
			TenantID:    scope.TenantID,
			settings:    fixedReader{scope: scope},
			ruleCatalog: catalogue,
			alertStore:  store,
			metrics:     m,
			logger:      testLogger(),
		},
		store:   store,
		metrics: m,
		scope:   scope,
		ctx:     dbtx.WithTenant(context.Background(), scope.TenantID, false),
	}
}

// ingest drives one alert through the read-loop handler, which never fails.
func (f alertFixture) ingest(t *testing.T, msg *protocol.ControlMessage) {
	t.Helper()
	require.NoError(t, f.conn.handleAgentAlert(f.ctx, msg, defaultAlertPayloadLen))
}

// dropped waits for exactly one alert to be counted under reason; store outcomes land on the
// persist-slot goroutine.
func (f alertFixture) dropped(t *testing.T, reason string) {
	t.Helper()
	require.Eventuallyf(t, func() bool {
		return promtestutil.ToFloat64(f.metrics.EdgeTelemetryDropsTotal.WithLabelValues(reason)) == 1
	}, 2*time.Second, 5*time.Millisecond, "expected one drop counted under %s", reason)
	assert.Equal(t, uint64(1), f.conn.DroppedTelemetryCount(),
		"a refusal is counted once, under one reason")
}

// reachedStore waits for n alerts to arrive at the store and returns them.
func (f alertFixture) reachedStore(t *testing.T, n int) []alerts.Alert {
	t.Helper()
	var got []alerts.Alert
	require.Eventuallyf(t, func() bool {
		got = f.store.recorded()
		return len(got) == n
	}, 2*time.Second, 5*time.Millisecond, "expected %d alerts to reach the store", n)
	return got
}

// defaultAlertPayloadLen is a frame comfortably inside the alert path's bound.
const defaultAlertPayloadLen = 2048

// catalogueRule names a rule this build ships.
func catalogueRule(t *testing.T) (string, uint32) {
	t.Helper()
	catalogue, err := rules.Embedded()
	require.NoError(t, err)
	all := catalogue.All()
	require.NotEmpty(t, all, "the embedded catalogue must ship at least one rule")
	return all[0].ID, uint32(all[0].Version)
}

// deflated compresses a payload the way the agent's evidence codec does.
func deflated(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer, err := flate.NewWriter(&buf, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = writer.Write(raw)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return buf.Bytes()
}

// wellFormed is the alert every case starts from and then breaks in one place.
func wellFormed(t *testing.T) *protocol.ControlMessage {
	t.Helper()
	ruleID, version := catalogueRule(t)
	now := time.Now().UTC()
	severityValue := protocol.AlertSeverityCritical
	backfilled := false
	value := 98.2
	return &protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       uuid.New().String(),
		RuleID:        ruleID,
		RuleVersion:   version,
		Severity:      &severityValue,
		Metric:        "disk.used_percent",
		Value:         &value,
		WindowStartTS: now.Add(-5 * time.Minute).Unix(),
		WindowEndTS:   now.Unix(),
		ObservedTS:    now.Unix(),
		Backfilled:    &backfilled,
		EvidenceCodec: protocol.EvidenceCodec,
		Evidence:      deflated(t, []byte(`{"ranked":[]}`)),
	}
}

// broken returns the well-formed alert with exactly one thing changed.
func broken(t *testing.T, change func(*protocol.ControlMessage)) *protocol.ControlMessage {
	t.Helper()
	msg := wellFormed(t)
	change(msg)
	return msg
}

// severity sets the pointer field, keeping a stated Info distinct from an absent one.
func severity(s protocol.AlertSeverity) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) { msg.Severity = &s }
}

// stamped moves every timestamp on the alert to one instant.
func stamped(at time.Time) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) {
		msg.WindowStartTS, msg.WindowEndTS, msg.ObservedTS = at.Unix(), at.Unix(), at.Unix()
	}
}

// backfilledAt is stamped for a retroactive finding, which has the wider bound.
func backfilledAt(at time.Time) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) {
		stamped(at)(msg)
		yes := true
		msg.Backfilled = &yes
	}
}

func TestHandleAgentAlertAdmission(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	cases := []struct {
		name       string
		change     func(*protocol.ControlMessage)
		payloadLen int
		wantReason string
		// preIngest marks a bound applied before the ingest counter fires.
		preIngest bool
	}{
		{
			name:       "a well-formed alert is admitted",
			payloadLen: 1024,
		},
		{
			name:       "an alert at the evidence cap plus its envelope is admitted",
			payloadLen: protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes,
		},
		{
			name:       "an alert past the bound is refused and counted",
			payloadLen: protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes + 1,
			wantReason: alertDropPayloadTooLarge,
			preIngest:  true,
		},
		{
			name:   "info is a severity",
			change: severity(protocol.AlertSeverityInfo),
		},
		{
			name:   "warning is a severity",
			change: severity(protocol.AlertSeverityWarning),
		},
		{
			name:       "a severity outside the set is refused and counted",
			change:     severity(protocol.AlertSeverity("Catastrophic")),
			wantReason: alertDropSeverityUnknown,
		},
		{
			name:       "an absent severity is refused rather than assumed",
			change:     func(m *protocol.ControlMessage) { m.Severity = nil },
			wantReason: alertDropSeverityUnknown,
		},
		{
			name:       "an alert with no rule id is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleID = "" },
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert with no rule version is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleVersion = 0 },
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert with no window start is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.WindowStartTS = 0 },
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert whose window runs backwards is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.WindowEndTS = m.WindowStartTS - 1 },
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "a rule this build does not ship is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleID = "invented-by-the-endpoint" },
			wantReason: alertDropRuleUnknown,
		},
		{
			name:       "an observation with no timestamp is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.ObservedTS = 0 },
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			name:       "a live alert from a month ago is refused and counted",
			change:     stamped(now.Add(-30 * 24 * time.Hour)),
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			name:       "an alert stamped hours ahead of the server is refused and counted",
			change:     stamped(now.Add(7 * time.Hour)),
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			name:   "a finding five months out of history is admitted",
			change: backfilledAt(now.Add(-150 * 24 * time.Hour)),
		},
		{
			name:       "a finding older than an alert is kept for is refused and counted",
			change:     backfilledAt(now.Add(-400 * 24 * time.Hour)),
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			name: "an alert with no evidence is admitted",
			change: func(m *protocol.ControlMessage) {
				m.Evidence = nil
				m.EvidenceCodec = ""
			},
			payloadLen: 256,
		},
		{
			name:       "evidence under an unreadable codec is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.EvidenceCodec = "brotli-9" },
			wantReason: alertDropEvidenceCodecUnknown,
		},
		{
			name:       "evidence with no codec named is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.EvidenceCodec = "" },
			wantReason: alertDropEvidenceCodecUnknown,
		},
		{
			name:       "evidence that does not decode is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.Evidence = []byte("not deflate at all") },
			wantReason: alertDropEvidenceUndecodable,
		},
		{
			name: "evidence that inflates past the composition is refused and counted",
			change: func(m *protocol.ControlMessage) {
				m.Evidence = deflated(t, make([]byte, maxEvidenceInflatedBytes+1))
			},
			wantReason: alertDropEvidenceUndecodable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := alertConn(t)
			msg := wellFormed(t)
			if tc.change != nil {
				tc.change(msg)
			}
			payloadLen := tc.payloadLen
			if payloadLen == 0 {
				payloadLen = 512
			}
			require.NoError(t, f.conn.handleAgentAlert(f.ctx, msg, payloadLen))

			if tc.wantReason == "" {
				f.reachedStore(t, 1)
				assert.Zero(t, f.conn.DroppedTelemetryCount())
				return
			}

			f.dropped(t, tc.wantReason)
			assert.Empty(t, f.store.recorded(), "a refused alert must not reach the store")

			wantIngested := float64(1)
			if tc.preIngest {
				wantIngested = 0
			}
			assert.InDelta(t, wantIngested, promtestutil.ToFloat64(
				f.metrics.EdgeTelemetryIngestedTotal.WithLabelValues(string(protocol.MsgAgentAlert))), 0,
				"a bound applied before the counter leaves nothing for the ledger to balance")
		})
	}
}

func TestAlertPayloadBoundIsItsOwn(t *testing.T) {
	t.Parallel()
	assert.Greater(t, maxAlertPayloadBytes, maxTelemetryPayloadBytes,
		"an alert carries evidence a telemetry message does not")
	assert.Equal(t, protocol.MaxEvidenceBytes+alertEnvelopeHeadroomBytes, maxAlertPayloadBytes,
		"the alert bound must be the evidence cap plus a stated envelope allowance")
}

func TestAgentAlertIsAWritePath(t *testing.T) {
	t.Parallel()
	assert.True(t, isWritePathMessage(protocol.MsgAgentAlert))

	tombstoned := &AgentConn{DeviceID: uuid.New(), logger: testLogger(), isTombstoned: func() bool { return true }}
	assert.True(t, tombstoned.rejectTombstonedWrite(wellFormed(t)))
	assert.Equal(t, uint64(1), tombstoned.DroppedTelemetryCount())
}

func TestAlertDropReasonsAreDistinct(t *testing.T) {
	t.Parallel()
	reasons := []string{
		alertDropPayloadTooLarge,
		alertDropSeverityUnknown,
		alertDropIdentityIncomplete,
		alertDropRuleUnknown,
		alertDropTimestampOutOfRange,
		alertDropEvidenceCodecUnknown,
		alertDropEvidenceUndecodable,
		alertDropOrganizationUnknown,
		alertDropOrganizationCeiling,
		alertDropDuplicate,
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		assert.False(t, seen[reason], "drop reason %q is used twice", reason)
		assert.True(t, strings.HasPrefix(reason, "alert_"),
			"an alert drop reason must be distinguishable from a telemetry one: %q", reason)
		seen[reason] = true
	}
}

func TestStoredSeverityKeepsTheWiresClosedSet(t *testing.T) {
	t.Parallel()
	cases := map[protocol.AlertSeverity]alerts.Severity{
		protocol.AlertSeverityInfo:     alerts.SeverityInfo,
		protocol.AlertSeverityWarning:  alerts.SeverityWarning,
		protocol.AlertSeverityCritical: alerts.SeverityCritical,
	}
	for wire, stored := range cases {
		got, ok := storedSeverity(&wire)
		assert.True(t, ok, "%q is one of the three", wire)
		assert.Equal(t, stored, got)
	}

	unknown := protocol.AlertSeverity("Catastrophic")
	_, ok := storedSeverity(&unknown)
	assert.False(t, ok)
	_, ok = storedSeverity(nil)
	assert.False(t, ok, "an absent severity is not a severity")
}

func TestAnAlertForARuleTheCustomerStoppedIsRefusedAndCounted(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	f.conn.wantedEventRules = map[string]struct{}{"linux-hung-task": {}}

	stopped := broken(t, func(m *protocol.ControlMessage) { m.RuleID = "linux-oom-kill" })
	f.ingest(t, stopped)

	f.dropped(t, alertDropRuleStopped)
}

func TestStoppingOneRuleDoesNotSilenceTheRest(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	f.conn.wantedEventRules = map[string]struct{}{"linux-hung-task": {}}

	f.ingest(t, wellFormed(t))
	require.Len(t, f.reachedStore(t, 1), 1,
		"a rule about a reading is stopped by never being sent, so its alerts still arrive")
}

func TestAConnectionToldNothingAdmitsEveryRule(t *testing.T) {
	t.Parallel()

	f := alertConn(t)
	require.Nil(t, f.conn.wantedEventRules)

	f.ingest(t, broken(t, func(m *protocol.ControlMessage) { m.RuleID = "linux-oom-kill" }))
	require.Len(t, f.reachedStore(t, 1), 1)
}
