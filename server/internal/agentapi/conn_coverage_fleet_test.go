package agentapi

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

type fleetCoverageFunc func(context.Context) (int, map[string]int, error)

func (f fleetCoverageFunc) FleetCoverage(ctx context.Context) (int, map[string]int, error) {
	return f(ctx)
}

func staticFleet(size int, blind map[string]int) fleetCoverageFunc {
	return func(context.Context) (int, map[string]int, error) { return size, blind, nil }
}

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

func TestFleetRuleCoverageWithoutAStoreIsEmpty(t *testing.T) {
	t.Parallel()

	s := NewAgentServer(AgentServerConfig{Logger: testLogger()})
	s.coverage.Report(dev(1), active("disk-critical"))

	got, err := s.FleetRuleCoverage(context.Background())
	require.NoError(t, err)
	assert.Empty(t, got, "with no fleet to measure against there is no split to report")
}

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
