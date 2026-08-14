package agentapi

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// What the server admits from an untrusted endpoint, and what it counts when it
// refuses. An alert is the only carrier of the detail behind a signal, so every
// refusal here is an incident nobody will ever be able to reconstruct — which is
// why each one is counted under a reason rather than dropped quietly.

func alertConn() *AgentConn {
	return &AgentConn{DeviceID: uuid.New(), logger: testLogger()}
}

// wellFormed is the alert every case below starts from and then breaks in one
// place, so a case's name is the only thing that differs from an accepted alert.
func wellFormed() *protocol.ControlMessage {
	severity := protocol.AlertSeverityCritical
	backfilled := false
	value := 41.5
	return &protocol.ControlMessage{
		Type:          protocol.MsgAgentAlert,
		AlertID:       "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0",
		RuleID:        "disk-latency-sustained",
		RuleVersion:   3,
		Severity:      &severity,
		Metric:        "disk.await_ms",
		Value:         &value,
		WindowStartTS: 1700000000,
		WindowEndTS:   1700000300,
		ObservedTS:    1700000305,
		Backfilled:    &backfilled,
		EvidenceCodec: "deflate-1",
		Evidence:      []byte{0x01, 0x02, 0x03},
	}
}

// broken returns the well-formed alert with exactly one thing changed, so each
// case below differs from an accepted alert in precisely the way its name says.
func broken(change func(*protocol.ControlMessage)) *protocol.ControlMessage {
	msg := wellFormed()
	change(msg)
	return msg
}

// severity is a small helper because the field is a pointer: a stated Info has
// to be distinguishable from an absent severity, which is the whole reason it is
// a pointer on the wire.
func severity(s protocol.AlertSeverity) func(*protocol.ControlMessage) {
	return func(msg *protocol.ControlMessage) { msg.Severity = &s }
}

func TestHandleAgentAlertAdmission(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		msg        *protocol.ControlMessage
		payloadLen int
		wantDrops  uint64
	}{
		{
			name:       "a well-formed alert is admitted",
			msg:        wellFormed(),
			payloadLen: 1024,
		},
		{
			// The evidence cap and the envelope are separate budgets. An alert
			// carrying the largest evidence the contract allows is exactly the
			// alert that matters most, so the bound has to leave room for the
			// envelope around it.
			name:       "an alert at the evidence cap plus its envelope is admitted",
			msg:        wellFormed(),
			payloadLen: protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes,
		},
		{
			name:       "an alert past the bound is refused and counted",
			msg:        wellFormed(),
			payloadLen: protocol.MaxEvidenceBytes + alertEnvelopeHeadroomBytes + 1,
			wantDrops:  1,
		},
		{
			name:       "info is a severity",
			msg:        broken(severity(protocol.AlertSeverityInfo)),
			payloadLen: 512,
		},
		{
			name:       "warning is a severity",
			msg:        broken(severity(protocol.AlertSeverityWarning)),
			payloadLen: 512,
		},
		{
			// The set is closed. A severity nothing downstream can render would
			// be stored as an incident nobody knows how to present.
			name:       "a severity outside the set is refused and counted",
			msg:        broken(severity(protocol.AlertSeverity("Catastrophic"))),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			name:       "an absent severity is refused rather than assumed",
			msg:        broken(func(m *protocol.ControlMessage) { m.Severity = nil }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			// (device, rule, version, window start) is what lets a reconnect
			// replay resolve to the row it already wrote. An alert missing any
			// part of it cannot be deduplicated, so it is refused rather than
			// stored under a null that would duplicate on the next reconnect.
			name:       "an alert with no rule id is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.RuleID = "" }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			name:       "an alert with no rule version is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.RuleVersion = 0 }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			name:       "an alert with no window start is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.WindowStartTS = 0 }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			name:       "an alert whose window runs backwards is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.WindowEndTS = m.WindowStartTS - 1 }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			// Evidence is optional: a device that had nothing to attach still
			// says the machine is in trouble, and that is the part nothing else
			// can reconstruct.
			name: "an alert with no evidence is admitted",
			msg: broken(func(m *protocol.ControlMessage) {
				m.Evidence = nil
				m.EvidenceCodec = ""
			}),
			payloadLen: 256,
		},
		{
			// A codec the server cannot read means the blob is unreadable, and
			// storing an unreadable blob beside an alert is worse than storing
			// none: it reads as evidence that exists.
			name:       "evidence under an unreadable codec is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.EvidenceCodec = "brotli-9" }),
			payloadLen: 512,
			wantDrops:  1,
		},
		{
			name:       "evidence with no codec named is refused and counted",
			msg:        broken(func(m *protocol.ControlMessage) { m.EvidenceCodec = "" }),
			payloadLen: 512,
			wantDrops:  1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := alertConn()
			require.NoError(t, conn.handleAgentAlert(context.Background(), tc.msg, tc.payloadLen))
			assert.Equal(t, tc.wantDrops, conn.DroppedTelemetryCount())
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
	assert.True(t, tombstoned.rejectTombstonedWrite(wellFormed()))
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
		alertDropEvidenceCodecUnknown,
	}
	seen := map[string]bool{}
	for _, reason := range reasons {
		assert.False(t, seen[reason], "drop reason %q is used twice", reason)
		assert.True(t, strings.HasPrefix(reason, "alert_"),
			"an alert drop reason must be distinguishable from a telemetry one: %q", reason)
		seen[reason] = true
	}
}
