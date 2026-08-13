package agentapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
			// persisted stands in for the rule_coverage_unsupported table,
			// maintained from exactly the deltas the production path writes.
			persisted := newFakeUnsupported()
			for _, report := range tc.reports {
				persisted.apply(report.device, store.Report(report.device, report.entries))
			}
			// Forgetting a machine is a liveness event. It must not touch the
			// durable rows: an offline container that cannot evaluate a rule is
			// still a hole in the estate.
			for _, device := range tc.forget {
				store.Forget(device)
			}

			got := store.Aggregate(tc.fleet, persisted.counts())
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

	assert.Len(t, store.Aggregate(1, nil), maxRuleCoverageEntries,
		"a device cannot make the server hold more rule ids than it could ever be pushed")
}

func TestAgentServer_RuleCoverageReadsTheStore(t *testing.T) {
	t.Parallel()
	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	s.coverage.Report(dev(1), active("disk-critical"))

	got := s.RuleCoverage(context.Background(), uuid.New(), 3)
	assert.Equal(t, RuleCoverageCounts{Active: 1, Unknown: 2}, got["disk-critical"])
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
			// The durable third of coverage goes through the connection's own
			// write-through path, so this exercises what production does rather
			// than only what the in-memory store remembers.
			persisted := newRecordingUnsupportedStore()
			ac.ruleCoverage = persisted

			msg := *tc.msg
			msg.Type = protocol.MsgAgentHealthSummary
			msg.TS = time.Now().Unix()
			writeControlMsg(t, ac.codec, buf, &msg)
			require.NoError(t, ac.handleControl(dbtx.WithDefaultTenant(context.Background(), false)))

			assert.Equal(t, tc.wantDrops, ac.telemetryDrops.Load())
			assert.Equal(t, tc.want, ac.coverage.Aggregate(1, persisted.counts()))
		})
	}
}

// fakeUnsupported stands in for the rule_coverage_unsupported table. It records
// every write, so a test can assert not only what is stored but that nothing was
// written when nothing changed.
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

// A machine reporting the same thing over and over costs nothing. Steady state
// is the overwhelming majority of reports, so this is the property that decides
// whether persisting coverage is affordable at fleet scale.
func TestRuleCoverageStore_WritesOnlyOnAChange(t *testing.T) {
	t.Parallel()

	store := NewRuleCoverageStore()
	persisted := newFakeUnsupported()

	report := func(entries []protocol.RuleCoverage) {
		persisted.apply(dev(1), store.Report(dev(1), entries))
	}

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

// A machine that stops mentioning a rule is no longer claiming it cannot
// evaluate it, so the stored row goes rather than lingering as a hole nobody is
// reporting.
func TestRuleCoverageStore_DroppingARuleClearsItsStoredHole(t *testing.T) {
	t.Parallel()

	store := NewRuleCoverageStore()
	persisted := newFakeUnsupported()

	persisted.apply(dev(1), store.Report(dev(1), []protocol.RuleCoverage{
		{RuleID: "io-stalled", State: protocol.RuleCoverageUnsupported},
		{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
	}))
	assert.Equal(t, map[string]int{"io-stalled": 1}, persisted.counts())

	persisted.apply(dev(1), store.Report(dev(1), active("disk-critical")))
	assert.Empty(t, persisted.counts())
}

// The durable half survives what memory does not. A machine that has gone
// offline is unknown for the rules it was evaluating, and still counted for the
// one it never could — which is exactly the difference between the two.
func TestRuleCoverageStore_OfflineMachineKeepsItsHole(t *testing.T) {
	t.Parallel()

	store := NewRuleCoverageStore()
	persisted := newFakeUnsupported()

	persisted.apply(dev(1), store.Report(dev(1), []protocol.RuleCoverage{
		{RuleID: "io-stalled", State: protocol.RuleCoverageUnsupported},
		{RuleID: "disk-critical", State: protocol.RuleCoverageActive},
	}))
	store.Forget(dev(1))

	got := store.Aggregate(2, persisted.counts())
	assert.Equal(t, RuleCoverageCounts{Unsupported: 1, Unknown: 1}, got["io-stalled"],
		"a container that cannot read pressure is still a hole while it is offline")

	// A server that has just restarted holds nothing in memory, and still knows
	// the same thing, because that half was never in memory.
	fresh := NewRuleCoverageStore()
	got = fresh.Aggregate(2, persisted.counts())
	assert.Equal(t, RuleCoverageCounts{Unsupported: 1, Unknown: 1}, got["io-stalled"])

	// The rule that machine was evaluating is simply not named any more: with
	// nothing reporting it and nothing stored, the server has no fleet split to
	// state for it — and crucially it is never reported as still active.
	assert.NotContains(t, got, "disk-critical")
}

// recordingUnsupportedStore is fakeUnsupported behind the interface the agent
// connection writes through.
type recordingUnsupportedStore struct {
	mu   sync.Mutex
	rows *fakeUnsupported
}

func newRecordingUnsupportedStore() *recordingUnsupportedStore {
	return &recordingUnsupportedStore{rows: newFakeUnsupported()}
}

func (r *recordingUnsupportedStore) MarkUnsupported(_ context.Context, _, deviceID uuid.UUID, ruleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows.apply(deviceID, RuleCoverageDelta{NowUnsupported: []string{ruleID}})
	return nil
}

func (r *recordingUnsupportedStore) ClearUnsupported(_ context.Context, deviceID uuid.UUID, ruleID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows.apply(deviceID, RuleCoverageDelta{NowActive: []string{ruleID}})
	return nil
}

func (r *recordingUnsupportedStore) CountUnsupported(context.Context, uuid.UUID) (map[string]int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows.counts(), nil
}

func (r *recordingUnsupportedStore) counts() map[string]int {
	got, _ := r.CountUnsupported(context.Background(), uuid.Nil)
	return got
}

// writes reports how many times the store was actually written to.
func (r *recordingUnsupportedStore) writes() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows.writes
}

// The connection's write-through is the production path, so the zero-writes
// property has to hold there and not only in the store's own diff.
func TestAgentConn_PersistsCoverageOnlyOnAChange(t *testing.T) {
	t.Parallel()

	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.coverage = NewRuleCoverageStore()
	persisted := newRecordingUnsupportedStore()
	ac.ruleCoverage = persisted
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	require.True(t, ac.recordRuleCoverage(ctx, unsupported("io-stalled")))
	assert.Equal(t, 1, persisted.writes())

	for range 50 {
		require.True(t, ac.recordRuleCoverage(ctx, unsupported("io-stalled")))
	}
	assert.Equal(t, 1, persisted.writes(), "an unchanged report must not write")

	require.True(t, ac.recordRuleCoverage(ctx, active("io-stalled")))
	assert.Equal(t, 2, persisted.writes(), "the machine recovering is one write")
	assert.Empty(t, persisted.counts())
}

// Coverage accounting must never be able to fail a machine's health summary: a
// store that is down costs the count, not the report.
func TestAgentConn_SurvivesAFailingCoverageStore(t *testing.T) {
	t.Parallel()

	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.coverage = NewRuleCoverageStore()
	ac.ruleCoverage = failingUnsupportedStore{}

	assert.True(t, ac.recordRuleCoverage(dbtx.WithDefaultTenant(context.Background(), false),
		unsupported("io-stalled")),
		"the summary is still recorded when the durable write fails")
}

type failingUnsupportedStore struct{}

func (failingUnsupportedStore) MarkUnsupported(context.Context, uuid.UUID, uuid.UUID, string) error {
	return errors.New("database is down")
}

func (failingUnsupportedStore) ClearUnsupported(context.Context, uuid.UUID, string) error {
	return errors.New("database is down")
}

func (failingUnsupportedStore) CountUnsupported(context.Context, uuid.UUID) (map[string]int, error) {
	return nil, errors.New("database is down")
}
