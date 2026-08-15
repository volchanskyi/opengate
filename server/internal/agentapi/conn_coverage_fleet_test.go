package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// The fleet-wide fold of coverage, which is what the platform's own monitoring
// reads. Per-customer coverage answers "how much of Contoso's estate is this
// rule watching"; this answers "how much of everything", which is the question a
// staged rollout is judged on.

// fleetCoverageFunc is a whole install's coverage as one function.
type fleetCoverageFunc func(context.Context) (int, map[string]int, error)

func (f fleetCoverageFunc) FleetCoverage(ctx context.Context) (int, map[string]int, error) {
	return f(ctx)
}

// staticFleet answers with a fixed fleet size and blind-spot count.
func staticFleet(size int, blind map[string]int) fleetCoverageFunc {
	return func(context.Context) (int, map[string]int, error) { return size, blind, nil }
}

// TestFleetRuleCoverageIsTheWholeInstallSplitPerRule is the answer the gauge
// exports: four states per rule that add up to the fleet, keyed by the state
// names the series carry.
func TestFleetRuleCoverageIsTheWholeInstallSplitPerRule(t *testing.T) {
	t.Parallel()

	s := NewAgentServer(AgentServerConfig{
		Logger:        testLogger(),
		FleetCoverage: staticFleet(10, map[string]int{"io-stalled": 2}),
	})
	s.coverage.Report(dev(1), active("disk-critical"))
	s.coverage.Report(dev(2), active("disk-critical"))
	s.coverage.Report(dev(3), throttled("disk-critical"))

	got, err := s.FleetRuleCoverage(context.Background())
	require.NoError(t, err)

	assert.Equal(t, map[string]int{
		appmetrics.CoverageActive:      2,
		appmetrics.CoverageThrottled:   1,
		appmetrics.CoverageUnsupported: 0,
		appmetrics.CoverageUnknown:     7,
	}, got["disk-critical"], "the four states add up to the fleet")

	assert.Equal(t, map[string]int{
		appmetrics.CoverageActive:      0,
		appmetrics.CoverageThrottled:   0,
		appmetrics.CoverageUnsupported: 2,
		appmetrics.CoverageUnknown:     8,
	}, got["io-stalled"], "a standing hole is read from storage, so an offline machine keeps counting")
}

// TestFleetRuleCoverageReportsAnUnreadableStore rather than reporting a fleet of
// zero. Every machine reading as unknown is a real and alarming state; a store
// that is briefly down is not, and the gauge must not be told they are the same.
func TestFleetRuleCoverageReportsAnUnreadableStore(t *testing.T) {
	t.Parallel()

	s := NewAgentServer(AgentServerConfig{
		Logger: testLogger(),
		FleetCoverage: fleetCoverageFunc(func(context.Context) (int, map[string]int, error) {
			return 0, nil, errors.New("database is down")
		}),
	})
	s.coverage.Report(dev(1), active("disk-critical"))

	_, err := s.FleetRuleCoverage(context.Background())
	require.Error(t, err, "an unreadable fleet is reported, never rendered as an empty one")
}

// TestFleetRuleCoverageWithoutAStoreIsEmpty keeps a deployment wired without the
// durable half from claiming a fleet it cannot count.
func TestFleetRuleCoverageWithoutAStoreIsEmpty(t *testing.T) {
	t.Parallel()

	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	s.coverage.Report(dev(1), active("disk-critical"))

	got, err := s.FleetRuleCoverage(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got, "with no fleet to measure against there is no split to report")
}

// TestRuleCoverageCountsRenderTheWholeSplit keeps the rendering total. Three
// states rendered out of four would make a rule look like it was watching a
// smaller estate than it is, which is the exact failure the accounting exists to
// make impossible.
func TestRuleCoverageCountsRenderTheWholeSplit(t *testing.T) {
	t.Parallel()

	counts := RuleCoverageCounts{Active: 1, Throttled: 2, Unsupported: 3, Unknown: 4}

	assert.Equal(t, map[string]int{
		appmetrics.CoverageActive:      1,
		appmetrics.CoverageThrottled:   2,
		appmetrics.CoverageUnsupported: 3,
		appmetrics.CoverageUnknown:     4,
	}, counts.ByState())
}
