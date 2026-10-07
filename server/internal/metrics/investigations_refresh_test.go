package metrics

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestInvestigationsUpdaterReadsOncePerIntervalNeverPerScrape(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	m := NewMetrics(reg)
	m.SeedRuleVocabulary(shippedRules)

	var reads atomic.Int64
	src := InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) {
			reads.Add(1)
			return map[string]int{IncidentNew: 3}, 12, nil
		},
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			reads.Add(1)
			return map[string]map[string]int{"disk-critical": {CoverageActive: 9}}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	StartInvestigationsUpdater(ctx, m, src, discardLogger(), time.Hour)
	require.EqualValues(t, 2, reads.Load(), "one refresh reads each source exactly once")

	for range 50 {
		_, err := reg.Gather()
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, reads.Load(), "a scrape must never reach the database")
	require.InDelta(t, 12, testutil.ToFloat64(m.AlertsOpen), 0, "the scrape still reads the refreshed value")
}

func TestInvestigationsUpdaterKeepsTheLastAnswerOnError(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	m.SeedRuleVocabulary(shippedRules)
	m.AlertsOpen.Set(42)
	m.IncidentsOpen.WithLabelValues(IncidentNew).Set(7)
	m.RuleCoverage.WithLabelValues("disk-critical", CoverageActive).Set(40)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	StartInvestigationsUpdater(ctx, m, InvestigationSource{
		OpenInvestigations: func(context.Context) (map[string]int, int, error) {
			return nil, 0, errors.New("database unreachable")
		},
		FleetRuleCoverage: func(context.Context) (map[string]map[string]int, error) {
			return nil, errors.New("database unreachable")
		},
	}, discardLogger(), time.Hour)

	require.InDelta(t, 42, testutil.ToFloat64(m.AlertsOpen), 0)
	require.InDelta(t, 7, testutil.ToFloat64(m.IncidentsOpen.WithLabelValues(IncidentNew)), 0)
	require.InDelta(t, 40, testutil.ToFloat64(m.RuleCoverage.WithLabelValues("disk-critical", CoverageActive)), 0)
}

func TestInvestigationsUpdaterStopsOnCancel(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartInvestigationsUpdater(ctx, m, InvestigationSource{}, discardLogger(), time.Millisecond)
		close(done)
	}()

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, 5*time.Millisecond, "the updater returns when its context is cancelled")
}

func TestInvestigationsUpdaterToleratesAnUnwiredSource(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	m.SeedRuleVocabulary(shippedRules)

	require.NotPanics(t, func() {
		refreshInvestigations(context.Background(), m, InvestigationSource{}, discardLogger())
	})
}
