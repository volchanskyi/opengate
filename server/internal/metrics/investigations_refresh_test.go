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

// What the refresh loop has to guarantee, as distinct from what the series it
// writes look like.
//
// One property carries the rest: the database is read on the loop's own timer
// and never on a scrape. These are counts over tables that only grow, and
// /metrics is scraped by more than one thing, so a gauge computed inside the
// collector puts a full aggregate on every scrape of an endpoint nobody
// controls the rate of.

// TestInvestigationsUpdaterReadsOncePerIntervalNeverPerScrape is the bound the
// gauges need. These are counts over tables that only grow, so computing them
// inside the collector would put a full aggregate on every Prometheus interval —
// and on every other scrape of the same endpoint besides.
//
// Asserted as a read count rather than as elapsed time: a slow query and a query
// per scrape are different defects, and only one of them is this one.
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

	// A cancelled context runs the boot refresh and returns, which is one
	// interval's worth of work and nothing more.
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

// TestInvestigationsUpdaterKeepsTheLastAnswerOnError prefers a stale count to a
// zero. A database that is briefly unreachable is not an empty triage queue, and
// the alert rule watching these gauges must not fire on the difference.
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

// TestInvestigationsUpdaterStopsOnCancel keeps the loop from outliving the
// process's shutdown.
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

// TestInvestigationsUpdaterToleratesAnUnwiredSource keeps a deployment without
// an alert store from panicking the metrics goroutine.
func TestInvestigationsUpdaterToleratesAnUnwiredSource(t *testing.T) {
	t.Parallel()

	m := NewMetrics(prometheus.NewRegistry())
	m.SeedRuleVocabulary(shippedRules)

	require.NotPanics(t, func() {
		refreshInvestigations(context.Background(), m, InvestigationSource{}, discardLogger())
	})
}
