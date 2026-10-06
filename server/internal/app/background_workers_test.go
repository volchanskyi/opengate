package app_test

import (
	"context"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/app"
	"github.com/volchanskyi/opengate/server/internal/testvm"
)

// backgroundSchedule is a complete schedule with cadences short enough to watch a pass.
func backgroundSchedule() app.BackgroundSchedule {
	return app.BackgroundSchedule{
		Gauges:         10 * time.Millisecond,
		DBSize:         10 * time.Millisecond,
		Investigations: 10 * time.Millisecond,
		Reconcile:      10 * time.Millisecond,
		SessionSweep:   10 * time.Millisecond,
		SessionGrace:   time.Minute,
		IncidentSweep:  10 * time.Millisecond,

		RetentionSweep: 10 * time.Millisecond,
		// A horizon this long keeps the run's own rows out of the sweep.
		RetentionHorizon: 365 * 24 * time.Hour,
	}
}

func TestStartBackgroundWorkersRefusesAScheduleWithAHoleInIt(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	incomplete := backgroundSchedule()
	incomplete.IncidentSweep = 0

	err = assembly.StartBackgroundWorkers(context.Background(), incomplete)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IncidentSweep")
}

func TestStartBackgroundWorkersRunsThePeriodicWorkers(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	require.NoError(t, assembly.StartBackgroundWorkers(ctx, backgroundSchedule()))

	// The size query can stay in flight while other packages use the database, so the wait is generous.
	assert.Eventually(t, func() bool {
		return promtestutil.ToFloat64(assembly.Metrics.DBSizeBytes) > 0
	}, time.Minute, 20*time.Millisecond, "no worker ever measured the database")
}

func TestStartBackgroundWorkersRunsTheOptionalWorkersToo(t *testing.T) {
	t.Parallel()

	cfg := baseConfig(t)
	cfg.VictoriaMetricsURL = testvm.BaseURL(t)
	cfg.GitHubRepo = "volchanskyi/opengate"

	assembly, err := app.Build(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, assembly.Reconciler, "the reconciliation sweep has something to sweep")

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	assert.NoError(t, assembly.StartBackgroundWorkers(ctx, backgroundSchedule()))
}
