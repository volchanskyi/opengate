package agentapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Coverage answers one question per rule: how much of the fleet is it actually
// watching. Every device is exactly one of active, unsupported or unknown, and
// the three always add up to the fleet — a rule that quietly evaluates on half
// an estate while reading as healthy is the failure this accounting exists to
// make impossible.

// active and unsupported build a one-rule report, the shape most cases need.
func active(ruleID string) []protocol.RuleCoverage {
	return []protocol.RuleCoverage{{RuleID: ruleID, State: protocol.RuleCoverageActive}}
}

func unsupported(ruleID string) []protocol.RuleCoverage {
	return []protocol.RuleCoverage{{RuleID: ruleID, State: protocol.RuleCoverageUnsupported}}
}

// dev returns a stable device id per index so a table reads deterministically.
func dev(n int) protocol.DeviceID {
	return uuid.NewSHA1(uuid.Nil, fmt.Appendf(nil, "coverage-device-%d", n))
}

// coverageReport is one device's report inside a case's setup sequence.
type coverageReport struct {
	device  protocol.DeviceID
	entries []protocol.RuleCoverage
}

func TestRuleCoverageStore_Aggregate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		reports []coverageReport
		forget  []protocol.DeviceID
		fleet   int
		want    map[string]RuleCoverageCounts
	}{
		{
			name: "three of five devices report; the rest are unknown",
			reports: []coverageReport{
				{dev(1), active("disk-critical")},
				{dev(2), active("disk-critical")},
				{dev(3), unsupported("disk-critical")},
			},
			fleet: 5,
			want:  map[string]RuleCoverageCounts{"disk-critical": {Active: 2, Unsupported: 1, Unknown: 2}},
		},
		{
			// A machine that drops off does not vanish from the accounting — it
			// becomes unknown, which is exactly what it is: nobody knows what
			// that rule is doing on a machine that is not there.
			name: "a device that disconnects moves from active to unknown",
			reports: []coverageReport{
				{dev(1), active("disk-critical")},
				{dev(2), active("disk-critical")},
				{dev(3), unsupported("disk-critical")},
			},
			forget: []protocol.DeviceID{dev(1)},
			fleet:  5,
			want:   map[string]RuleCoverageCounts{"disk-critical": {Active: 1, Unsupported: 1, Unknown: 3}},
		},
		{
			// One machine, two rules, different answers: the disk rule evaluates,
			// the stall rule cannot because this kernel publishes no pressure
			// information.
			name: "each rule is counted separately",
			reports: []coverageReport{{dev(1), []protocol.RuleCoverage{
				{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
				{RuleID: "io-stalled", State: protocol.RuleCoverageUnsupported},
			}}},
			fleet: 2,
			want: map[string]RuleCoverageCounts{
				"disk-critical": {Active: 1, Unknown: 1},
				"io-stalled":    {Unsupported: 1, Unknown: 1},
			},
		},
		{
			name: "the latest report replaces the device's previous answer",
			reports: []coverageReport{
				{dev(1), unsupported("disk-critical")},
				{dev(1), active("disk-critical")},
			},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{"disk-critical": {Active: 1}},
		},
		{
			name: "a rule nobody evaluates any more leaves the accounting",
			reports: []coverageReport{
				{dev(1), active("retired")},
				{dev(1), active("current")},
			},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{"current": {Active: 1}},
		},
		{
			// A fleet count read while more machines than it names are connected
			// can lag behind them. The answer is zero unknown devices, never a
			// negative one.
			name: "unknown never goes negative",
			reports: []coverageReport{
				{dev(1), active("disk-critical")},
				{dev(2), active("disk-critical")},
				{dev(3), active("disk-critical")},
			},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{"disk-critical": {Active: 3}},
		},
		{
			name: "a rule id that cannot be a label is refused, its neighbours are not",
			reports: []coverageReport{{dev(1), []protocol.RuleCoverage{
				{RuleID: "   ", State: protocol.RuleCoverageActive},
				{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
			}}},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{"disk-critical": {Active: 1}},
		},
		{
			// A state this server cannot read leaves the device unknown for that
			// rule rather than being guessed into one of the two it knows.
			name: "an unreadable state is counted as neither",
			reports: []coverageReport{
				{dev(1), []protocol.RuleCoverage{{RuleID: "disk-critical", State: "Sideways"}}},
			},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{},
		},
		{
			name:  "nothing reported yet",
			fleet: 10,
			want:  map[string]RuleCoverageCounts{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := NewRuleCoverageStore()
			for _, report := range tc.reports {
				store.Report(report.device, report.entries)
			}
			for _, device := range tc.forget {
				store.Forget(device)
			}

			got := store.Aggregate(tc.fleet)
			assert.Equal(t, tc.want, got)
			for ruleID, counts := range got {
				total := counts.Active + counts.Unsupported + counts.Unknown
				assert.GreaterOrEqual(t, total, tc.fleet,
					"%s: active + unsupported + unknown must account for the whole fleet", ruleID)
			}
		})
	}
}

func TestRuleCoverageStore_BoundsUntrustedInput(t *testing.T) {
	t.Parallel()
	store := NewRuleCoverageStore()

	entries := make([]protocol.RuleCoverage, 0, maxRuleCoverageEntries*2)
	for i := range maxRuleCoverageEntries * 2 {
		entries = append(entries, protocol.RuleCoverage{
			RuleID: fmt.Sprintf("rule-%03d", i),
			State:  protocol.RuleCoverageActive,
		})
	}
	store.Report(dev(1), entries)

	assert.Len(t, store.Aggregate(1), maxRuleCoverageEntries,
		"a device cannot make the server hold more rule ids than it could ever be pushed")
}

func TestAgentServer_RuleCoverageReadsTheStore(t *testing.T) {
	t.Parallel()
	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	s.coverage.Report(dev(1), active("disk-critical"))

	assert.Equal(t, RuleCoverageCounts{Active: 1, Unknown: 2}, s.RuleCoverage(3)["disk-critical"])
}

func TestAgentConn_HealthSummaryCoverage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		msg       *protocol.ControlMessage
		wantDrops uint64
		want      map[string]RuleCoverageCounts
	}{
		{
			name: "coverage is recorded alongside a sampler computation",
			msg: &protocol.ControlMessage{
				NodeAnomalyRate: 0.1,
				SamplerVersion:  "sysinfo-k2",
				RuleCoverage: []protocol.RuleCoverage{
					{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
					{RuleID: "io-stalled", State: protocol.RuleCoverageUnsupported},
				},
			},
			want: map[string]RuleCoverageCounts{
				"disk-critical": {Active: 1},
				"io-stalled":    {Unsupported: 1},
			},
		},
		{
			// A calm machine's summary says what every rule is doing on it and
			// nothing else. That is state the server now holds, so counting it as
			// a discarded message would put a lie in the ledger.
			name:      "a summary carrying only coverage is not a drop",
			msg:       &protocol.ControlMessage{RuleCoverage: active("disk-critical")},
			wantDrops: 0,
			want:      map[string]RuleCoverageCounts{"disk-critical": {Active: 1}},
		},
		{
			name:      "a summary carrying nothing at all is still a drop",
			msg:       &protocol.ControlMessage{},
			wantDrops: 1,
			want:      map[string]RuleCoverageCounts{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ac, buf := newTestAgentConn(t, uuid.New(), nil)
			ac.telemetry = &recordingTelemetryWriter{calls: make(chan telemetryWriteCall, 1)}
			ac.coverage = NewRuleCoverageStore()

			msg := *tc.msg
			msg.Type = protocol.MsgAgentHealthSummary
			msg.TS = time.Now().Unix()
			writeControlMsg(t, ac.codec, buf, &msg)
			require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

			assert.Equal(t, tc.wantDrops, ac.telemetryDrops.Load())
			assert.Equal(t, tc.want, ac.coverage.Aggregate(1))
		})
	}
}
