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

// What the server admits from an untrusted endpoint, and what it counts when it
// refuses. An alert is the only carrier of the detail behind a signal, so every
// refusal here is an incident nobody will ever be able to reconstruct — which is
// why each one is counted under a reason rather than dropped quietly.

// alertFixture is a connection wired the way production wires one — a customer
// the machine belongs to, the compiled-in catalogue, a store — plus the pieces a
// case needs to read back what happened.
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

// ingest drives one alert through the read-loop handler. It never fails: a
// refused alert is a fact about that message, not a reason to tear down a
// control channel that also carries this device's remote-management paths.
func (f alertFixture) ingest(t *testing.T, msg *protocol.ControlMessage) {
	t.Helper()
	require.NoError(t, f.conn.handleAgentAlert(f.ctx, msg, defaultAlertPayloadLen))
}

// dropped waits for exactly one alert to be counted under reason. The wait is
// needed because a store outcome lands on the persist-slot goroutine rather than
// the read loop.
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

// defaultAlertPayloadLen is a frame comfortably inside the alert path's bound,
// so a case that is not about the bound never trips it.
const defaultAlertPayloadLen = 2048

// catalogueRule names a rule this build actually ships, so a case meaning to
// exercise something else is not quietly refused for inventing a rule.
func catalogueRule(t *testing.T) (string, uint32) {
	t.Helper()
	catalogue, err := rules.Embedded()
	require.NoError(t, err)
	all := catalogue.All()
	require.NotEmpty(t, all, "the embedded catalogue must ship at least one rule")
	return all[0].ID, uint32(all[0].Version)
}

// deflated compresses a payload the way the agent's evidence codec does, so a
// case carrying evidence carries something that actually reads back.
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

// wellFormed is the alert every case below starts from and then breaks in one
// place, so a case's name is the only thing that differs from a stored alert.
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

// broken returns the well-formed alert with exactly one thing changed, so each
// case below differs from an accepted alert in precisely the way its name says.
func broken(t *testing.T, change func(*protocol.ControlMessage)) *protocol.ControlMessage {
	t.Helper()
	msg := wellFormed(t)
	change(msg)
	return msg
}

// severity is a small helper because the field is a pointer: a stated Info has
// to be distinguishable from an absent severity, which is the whole reason it is
// a pointer on the wire.
func severity(s protocol.AlertSeverity) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) { msg.Severity = &s }
}

// stamped moves every timestamp on the alert to one instant, which is how the
// clock-window cases move an alert outside the range its kind is allowed.
func stamped(at time.Time) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) {
		msg.WindowStartTS, msg.WindowEndTS, msg.ObservedTS = at.Unix(), at.Unix(), at.Unix()
	}
}

func TestHandleAgentAlertAdmission(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	cases := []struct {
		name string
		// change breaks the well-formed alert in exactly one place. Nil leaves
		// it whole, which is what an admitted case is.
		change     func(*protocol.ControlMessage)
		payloadLen int
		// wantReason is empty for an alert that is admitted and reaches the
		// store, and otherwise names the single reason it was refused under.
		wantReason string
		// preIngest marks a bound applied before the ingest counter fires, so
		// the message was never counted as ingested at all.
		preIngest bool
	}{
		{
			name:       "a well-formed alert is admitted",
			payloadLen: 1024,
		},
		{
			// The evidence cap and the envelope are separate budgets. An alert
			// carrying the largest evidence the contract allows is exactly the
			// alert that matters most, so the bound has to leave room for the
			// envelope around it.
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
			name:       "info is a severity",
			change:     severity(protocol.AlertSeverityInfo),
			payloadLen: 512,
		},
		{
			name:       "warning is a severity",
			change:     severity(protocol.AlertSeverityWarning),
			payloadLen: 512,
		},
		{
			// The set is closed. A severity nothing downstream can render would
			// be stored as an incident nobody knows how to present.
			name:       "a severity outside the set is refused and counted",
			change:     severity(protocol.AlertSeverity("Catastrophic")),
			payloadLen: 512,
			wantReason: alertDropSeverityUnknown,
		},
		{
			name:       "an absent severity is refused rather than assumed",
			change:     func(m *protocol.ControlMessage) { m.Severity = nil },
			payloadLen: 512,
			wantReason: alertDropSeverityUnknown,
		},
		{
			// (device, rule, version, window start) is what lets a reconnect
			// replay resolve to the row it already wrote. An alert missing any
			// part of it cannot be deduplicated, so it is refused rather than
			// stored under a null that would duplicate on the next reconnect.
			name:       "an alert with no rule id is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleID = "" },
			payloadLen: 512,
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert with no rule version is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleVersion = 0 },
			payloadLen: 512,
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert with no window start is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.WindowStartTS = 0 },
			payloadLen: 512,
			wantReason: alertDropIdentityIncomplete,
		},
		{
			name:       "an alert whose window runs backwards is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.WindowEndTS = m.WindowStartTS - 1 },
			payloadLen: 512,
			wantReason: alertDropIdentityIncomplete,
		},
		{
			// A rule this build has no definition for cannot be rendered,
			// grouped or retuned. Stored, it would be a row a technician can see
			// and nobody can act on.
			name:       "a rule this build does not ship is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.RuleID = "invented-by-the-endpoint" },
			payloadLen: 512,
			wantReason: alertDropRuleUnknown,
		},
		{
			name:       "an observation with no timestamp is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.ObservedTS = 0 },
			payloadLen: 512,
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			// Refused rather than clamped: the window start is the alert's
			// identity, so pulling it to a bound would make the same alert
			// resolve to a different row on every reconnect and duplicate
			// itself instead of deduplicating.
			name:       "a live alert from a month ago is refused and counted",
			change:     stamped(now.Add(-30 * 24 * time.Hour)),
			payloadLen: 512,
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			name:       "an alert stamped hours ahead of the server is refused and counted",
			change:     stamped(now.Add(7 * time.Hour)),
			payloadLen: 512,
			wantReason: alertDropTimestampOutOfRange,
		},
		{
			// Evidence is optional: a device that had nothing to attach still
			// says the machine is in trouble, and that is the part nothing else
			// can reconstruct.
			name: "an alert with no evidence is admitted",
			change: func(m *protocol.ControlMessage) {
				m.Evidence = nil
				m.EvidenceCodec = ""
			},
			payloadLen: 256,
		},
		{
			// A codec the server cannot read means the blob is unreadable, and
			// storing an unreadable blob beside an alert is worse than storing
			// none: it reads as evidence that exists.
			name:       "evidence under an unreadable codec is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.EvidenceCodec = "brotli-9" },
			payloadLen: 512,
			wantReason: alertDropEvidenceCodecUnknown,
		},
		{
			name:       "evidence with no codec named is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.EvidenceCodec = "" },
			payloadLen: 512,
			wantReason: alertDropEvidenceCodecUnknown,
		},
		{
			// The codec named is one the server reads and the blob still is not
			// one, which the codec check alone cannot tell.
			name:       "evidence that does not decode is refused and counted",
			change:     func(m *protocol.ControlMessage) { m.Evidence = []byte("not deflate at all") },
			payloadLen: 512,
			wantReason: alertDropEvidenceUndecodable,
		},
		{
			name: "evidence that inflates past the composition is refused and counted",
			change: func(m *protocol.ControlMessage) {
				m.Evidence = deflated(t, make([]byte, maxEvidenceInflatedBytes+1))
			},
			payloadLen: 512,
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
			require.NoError(t, f.conn.handleAgentAlert(f.ctx, msg, tc.payloadLen))

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
	// The telemetry bound and the alert bound are different budgets on different
	// paths. Sizing the alert path from the telemetry one would put the largest
	// legal evidence plus its envelope over the line, and E11's "truncate, never
	// reject" would be defeated by a bound nobody had looked at.
	assert.Greater(t, maxAlertPayloadBytes, maxTelemetryPayloadBytes,
		"an alert carries evidence a telemetry message does not")
	assert.Equal(t, protocol.MaxEvidenceBytes+alertEnvelopeHeadroomBytes, maxAlertPayloadBytes,
		"the alert bound must be the evidence cap plus a stated envelope allowance")
}

func TestAgentAlertIsAWritePath(t *testing.T) {
	t.Parallel()
	// A purged device must not go on raising alerts: an alert becomes a stored
	// incident, which is tenant data the purge just removed.
	assert.True(t, isWritePathMessage(protocol.MsgAgentAlert))

	tombstoned := &AgentConn{DeviceID: uuid.New(), logger: testLogger(), isTombstoned: func() bool { return true }}
	assert.True(t, tombstoned.rejectTombstonedWrite(wellFormed(t)))
	assert.Equal(t, uint64(1), tombstoned.DroppedTelemetryCount())
}

func TestAlertDropReasonsAreDistinct(t *testing.T) {
	t.Parallel()
	// Each refusal names its own cause. One shared reason would make a fleet-wide
	// rollout bug and a single misbehaving device look like the same number.
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
	// One closed set, two spellings. The wire mirrors the Rust enum and the
	// database keeps the lower-cased form, so the mapping is a spelling rule
	// rather than a second vocabulary that could drift out of step.
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
