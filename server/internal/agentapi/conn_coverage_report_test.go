package agentapi

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// What a connection does with the coverage a machine reports: it records it, it
// writes the durable third of it through, and it never lets either fail the
// machine's health summary.

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
