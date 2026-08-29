// Periodic work, stated as an outcome of the assembled product rather than as a
// sweep's own unit test. Starting the workers belongs to whoever built the
// product, so what they do is observable from out here — which is the whole
// reason the binary does not own them.
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

// backgroundSchedule is a complete schedule with every cadence short enough for
// a test to watch a worker actually do a pass. Which cadence ships is the
// binary's decision and the binary's tests hold it; nothing here asserts one.
func backgroundSchedule() app.BackgroundSchedule {
	return app.BackgroundSchedule{
		Gauges:         10 * time.Millisecond,
		DBSize:         10 * time.Millisecond,
		Investigations: 10 * time.Millisecond,
		Reconcile:      10 * time.Millisecond,
		SessionSweep:   10 * time.Millisecond,
		SessionGrace:   time.Minute,
		IncidentSweep:  10 * time.Millisecond,
	}
}

// A schedule with a hole in it is a worker that never runs: a zero duration
// panics inside the ticker, on a goroutine nobody is watching, and the pass it
// was supposed to make simply never happens. The refusal names the field, and
// nothing is started.
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

// The whole product stood up, its periodic work started, and a pass observed as
// an outcome rather than asserted against a sweep's own unit test. The database
// size is measured by one of the workers before its first tick, so a gauge that
// has moved off zero is one of them having genuinely run.
func TestStartBackgroundWorkersRunsThePeriodicWorkers(t *testing.T) {
	t.Parallel()

	assembly, err := app.Build(context.Background(), baseConfig(t))
	require.NoError(t, err)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	require.NoError(t, assembly.StartBackgroundWorkers(ctx, backgroundSchedule()))

	assert.Eventually(t, func() bool {
		return promtestutil.ToFloat64(assembly.Metrics.DBSizeBytes) > 0
	}, 10*time.Second, 20*time.Millisecond, "no worker ever measured the database")
}

// The reconciliation sweep and the release-feed sync are the two workers that
// exist only when the assembly was given what they need. Both wired, the start
// still returns cleanly — a worker that panics on a dependency it was handed
// takes the process with it.
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
