package agentapi

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// active and unsupported build a one-rule report, the shape most cases need.
func active(ruleID string) []protocol.RuleCoverage {
	return []protocol.RuleCoverage{{RuleID: ruleID, State: protocol.RuleCoverageActive}}
}

func unsupported(ruleID string) []protocol.RuleCoverage {
	return []protocol.RuleCoverage{{RuleID: ruleID, State: protocol.RuleCoverageUnsupported}}
}

func throttled(ruleID string) []protocol.RuleCoverage {
	return []protocol.RuleCoverage{{RuleID: ruleID, State: protocol.RuleCoverageThrottled}}
}

// dev returns a stable device id per index.
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
			name: "an unreadable state is counted as neither",
			reports: []coverageReport{
				{dev(1), []protocol.RuleCoverage{{RuleID: "disk-critical", State: "Sideways"}}},
			},
			fleet: 1,
			want:  map[string]RuleCoverageCounts{},
		},
		{
			name: "a machine that throttled a rule is counted apart",
			reports: []coverageReport{
				{dev(1), active("disk-slow")},
				{dev(2), throttled("disk-slow")},
			},
			fleet: 4,
			want:  map[string]RuleCoverageCounts{"disk-slow": {Active: 1, Throttled: 1, Unknown: 2}},
		},
		{
			name: "a machine that cannot evaluate a rule is counted once, not twice",
			reports: []coverageReport{
				{dev(1), unsupported("io-stalled")},
				{dev(2), active("io-stalled")},
			},
			fleet: 2,
			want:  map[string]RuleCoverageCounts{"io-stalled": {Active: 1, Unsupported: 1}},
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
			// persisted mirrors the unsupported table from the deltas production writes.
			persisted := newFakeUnsupported()
			for _, report := range tc.reports {
				persisted.apply(report.device, store.Report(report.device, report.entries))
			}
			// Forgetting a machine leaves the durable rows untouched.
			for _, device := range tc.forget {
				store.Forget(device)
			}

			got := store.Aggregate(tc.fleet, persisted.counts())
			assert.Equal(t, tc.want, got)
			for ruleID, counts := range got {
				total := counts.Active + counts.Unsupported + counts.Throttled + counts.Unknown
				assert.GreaterOrEqual(t, total, tc.fleet,
					"%s: every state together must account for the whole fleet", ruleID)
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

	assert.Len(t, store.Aggregate(1, nil), maxRuleCoverageEntries,
		"a device cannot make the server hold more rule ids than it could ever be pushed")
}

// fakeUnsupported mirrors the persisted unsupported rows from the deltas applied to it.
type fakeUnsupported struct {
	rows   map[protocol.DeviceID]map[string]bool
	writes int
}

func newFakeUnsupported() *fakeUnsupported {
	return &fakeUnsupported{rows: make(map[protocol.DeviceID]map[string]bool)}
}

func (f *fakeUnsupported) apply(device protocol.DeviceID, delta RuleCoverageDelta) {
	for _, ruleID := range delta.NowUnsupported {
		if f.rows[device] == nil {
			f.rows[device] = make(map[string]bool)
		}
		f.rows[device][ruleID] = true
		f.writes++
	}
	for _, ruleID := range delta.NowActive {
		delete(f.rows[device], ruleID)
		f.writes++
	}
}

func (f *fakeUnsupported) counts() map[string]int {
	out := make(map[string]int)
	for _, rules := range f.rows {
		for ruleID := range rules {
			out[ruleID]++
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// coverageRig is a store paired with the persisted rows its deltas produce.
type coverageRig struct {
	store     *RuleCoverageStore
	persisted *fakeUnsupported
}

func newCoverageRig() coverageRig {
	return coverageRig{store: NewRuleCoverageStore(), persisted: newFakeUnsupported()}
}

// report applies device one's report and the delta it yields.
func (r coverageRig) report(entries []protocol.RuleCoverage) {
	r.persisted.apply(dev(1), r.store.Report(dev(1), entries))
}

// holeAndActive reports one unsupported rule beside one active rule.
func holeAndActive() []protocol.RuleCoverage {
	return []protocol.RuleCoverage{
		{RuleID: "io-stalled", State: protocol.RuleCoverageUnsupported},
		{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
	}
}

func TestRuleCoverageStore_WritesOnlyOnAChange(t *testing.T) {
	t.Parallel()

	rig := newCoverageRig()
	persisted := rig.persisted
	report := rig.report

	report(unsupported("io-stalled"))
	assert.Equal(t, 1, persisted.writes, "the first report of a hole is a write")
	assert.Equal(t, map[string]int{"io-stalled": 1}, persisted.counts())

	for range 100 {
		report(unsupported("io-stalled"))
	}
	assert.Equal(t, 1, persisted.writes, "saying the same thing again must cost nothing")

	report(active("io-stalled"))
	assert.Equal(t, 2, persisted.writes, "the hole closing is a write")
	assert.Empty(t, persisted.counts(), "and leaves no stored state behind")

	for range 100 {
		report(active("io-stalled"))
	}
	assert.Equal(t, 2, persisted.writes, "an evaluating machine keeps costing nothing")
}

func TestRuleCoverageStore_ThrottlingClearsAStoredHole(t *testing.T) {
	t.Parallel()

	rig := newCoverageRig()

	rig.report(unsupported("disk-slow"))
	require.Equal(t, map[string]int{"disk-slow": 1}, rig.persisted.counts())

	rig.report(throttled("disk-slow"))
	assert.Empty(t, rig.persisted.counts(), "a throttle is not a permanent hole in the estate")
	assert.Equal(t, RuleCoverageCounts{Throttled: 1},
		rig.store.Aggregate(1, rig.persisted.counts())["disk-slow"])
}

func TestRuleCoverageStore_DroppingARuleClearsItsStoredHole(t *testing.T) {
	t.Parallel()

	rig := newCoverageRig()

	rig.report(holeAndActive())
	assert.Equal(t, map[string]int{"io-stalled": 1}, rig.persisted.counts())

	rig.report(active("disk-critical"))
	assert.Empty(t, rig.persisted.counts())
}

func TestRuleCoverageStore_OfflineMachineKeepsItsHole(t *testing.T) {
	t.Parallel()

	rig := newCoverageRig()
	persisted := rig.persisted

	rig.report(holeAndActive())
	rig.store.Forget(dev(1))

	got := rig.store.Aggregate(2, persisted.counts())
	assert.Equal(t, RuleCoverageCounts{Unsupported: 1, Unknown: 1}, got["io-stalled"],
		"a container that cannot read pressure is still a hole while it is offline")

	fresh := NewRuleCoverageStore()
	got = fresh.Aggregate(2, persisted.counts())
	assert.Equal(t, RuleCoverageCounts{Unsupported: 1, Unknown: 1}, got["io-stalled"])

	assert.NotContains(t, got, "disk-critical")
}
