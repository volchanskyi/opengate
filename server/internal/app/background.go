package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// janitor is a periodic sweep: a pass that reclaims what accumulates, run by one shared loop.
type janitor struct {
	// every is how often the pass runs.
	every time.Duration
	// atBoot runs a pass before the first tick, for backlogs that already wait at process start.
	atBoot bool
	// sweep does one pass and reports how much it reclaimed.
	sweep func(context.Context) (int, error)
	// failed is logged on every error; found is called only when a pass reclaimed something.
	failed string
	found  func(reclaimed int)
}

// run sweeps until ctx is cancelled.
func (j janitor) run(ctx context.Context, logger *slog.Logger) {
	ticker := time.NewTicker(j.every)
	defer ticker.Stop()
	for {
		if j.atBoot {
			if reclaimed, err := j.sweep(ctx); err != nil {
				logger.Error(j.failed, "error", err)
			} else if reclaimed > 0 {
				j.found(reclaimed)
			}
		}
		j.atBoot = true
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// orphanSweeper reclaims telemetry series no device owns.
type orphanSweeper interface {
	Sweep(ctx context.Context) (int, error)
}

// startReconcileLoop runs the reconciliation sweep until ctx is cancelled.
func startReconcileLoop(ctx context.Context, every time.Duration, reconciler orphanSweeper, logger *slog.Logger) {
	janitor{
		every:  every,
		sweep:  reconciler.Sweep,
		failed: "reconcile sweep failed",
		found: func(purged int) {
			logger.Warn("reconcile sweep purged orphan telemetry", "count", purged)
		},
	}.run(ctx, logger)
}

// staleIncidentResolver closes the rooms whose hold has run out.
type staleIncidentResolver interface {
	ResolveStale(ctx context.Context, windows map[string]time.Duration) (int, error)
}

// startIncidentSweepLoop closes quiet incidents, starting with one pass at boot.
func startIncidentSweepLoop(
	ctx context.Context, every time.Duration, store staleIncidentResolver,
	windows map[string]time.Duration, logger *slog.Logger,
) {
	janitor{
		every:  every,
		atBoot: true,
		sweep:  func(ctx context.Context) (int, error) { return store.ResolveStale(ctx, windows) },
		failed: "incident auto-resolve sweep failed",
		found: func(resolved int) {
			logger.Info("auto-resolved quiet incidents", "count", resolved)
		},
	}.run(ctx, logger)
}

// expiredRowSweeper reclaims rows held longer than the horizon.
type expiredRowSweeper interface {
	SweepExpired(ctx context.Context, horizon time.Duration) (int, error)
}

// startRetentionSweepLoop removes alerts, evidence and closed rooms held past the horizon.
// It deletes customer records, so a pass that removed anything logs the count.
func startRetentionSweepLoop(
	ctx context.Context, every, horizon time.Duration,
	store expiredRowSweeper, logger *slog.Logger,
) {
	janitor{
		every:  every,
		atBoot: true,
		sweep:  func(ctx context.Context) (int, error) { return store.SweepExpired(ctx, horizon) },
		failed: "retention sweep failed",
		found: func(removed int) {
			logger.Info("reclaimed records past the retention horizon", "count", removed)
		},
	}.run(ctx, logger)
}

// ProductionGaugeInterval is how often the shipped binary reads the pool's statistics.
// The pool answers under a lock that every taken connection also holds, so it is read on a timer.
const ProductionGaugeInterval = 5 * time.Second

type BackgroundSchedule struct {
	// Gauges is how often the connection pool's statistics are read.
	Gauges time.Duration
	// DBSize is how often the database's on-disk size is measured.
	DBSize time.Duration
	// Investigations is how often the rule-pack and queue aggregates run.
	Investigations time.Duration
	// Reconcile is how often orphaned telemetry series are swept.
	Reconcile time.Duration
	// SessionSweep is how often unheld session rows are reclaimed; SessionGrace is their minimum age.
	SessionSweep time.Duration
	SessionGrace time.Duration
	// IncidentSweep is how often rooms whose hold has run out are closed.
	IncidentSweep time.Duration
	// RetentionSweep is how often expired records are removed; RetentionHorizon is their keep time.
	RetentionSweep   time.Duration
	RetentionHorizon time.Duration
}

// Validate refuses a schedule with a non-positive field, naming it.
// A zero duration would panic inside time.NewTicker on a background goroutine.
func (s BackgroundSchedule) Validate() error {
	for name, d := range map[string]time.Duration{
		"Gauges":           s.Gauges,
		"DBSize":           s.DBSize,
		"Investigations":   s.Investigations,
		"Reconcile":        s.Reconcile,
		"SessionSweep":     s.SessionSweep,
		"SessionGrace":     s.SessionGrace,
		"IncidentSweep":    s.IncidentSweep,
		"RetentionSweep":   s.RetentionSweep,
		"RetentionHorizon": s.RetentionHorizon,
	} {
		if d <= 0 {
			return fmt.Errorf("app: BackgroundSchedule.%s must be positive", name)
		}
	}
	return nil
}

// StartBackgroundWorkers launches every periodic worker and returns immediately.
// Each stops when ctx is cancelled.
func (a *Assembly) StartBackgroundWorkers(ctx context.Context, sched BackgroundSchedule) error {
	if err := sched.Validate(); err != nil {
		return err
	}

	go appmetrics.StartDBSizeUpdater(ctx, a.Metrics, a.Store, a.Logger, sched.DBSize)
	go appmetrics.StartDBPoolUpdater(ctx, a.Metrics,
		appmetrics.SQLPoolStatter(a.Store.PoolStats), sched.Gauges)

	// Both gauges aggregate whole tables, so a timer refreshes them off the scrape path.
	go appmetrics.StartInvestigationsUpdater(ctx, a.Metrics, appmetrics.InvestigationSource{
		OpenInvestigations: a.Alerts.OpenInvestigations,
		FleetRuleCoverage:  a.Agents.FleetRuleCoverage,
	}, a.Logger, sched.Investigations)

	// A nil Reconciler stays out of the loop, since a nil pointer as a port is a non-nil interface.
	if a.Reconciler != nil {
		go startReconcileLoop(ctx, sched.Reconcile, a.Reconciler, a.Logger)
	}

	sweeper := session.NewSweeper(a.Sessions, liveRelayTokens(a.Relay), sched.SessionGrace, a.Logger)
	go startSessionSweepLoop(ctx, sched.SessionSweep, sweeper, a.Logger)

	go startIncidentSweepLoop(ctx, sched.IncidentSweep, a.Alerts, groupWindows(a.Rules), a.Logger)

	go startRetentionSweepLoop(ctx, sched.RetentionSweep, sched.RetentionHorizon, a.Alerts, a.Logger)

	if a.githubRepo != "" {
		go updater.StartPeriodicSync(ctx, a.githubRepo, 0, a.SigningKeys, a.Manifests, a.Logger)
	}
	return nil
}

// groupWindows maps each shipped rule to its grouping window, how long a quiet room stays open.
func groupWindows(catalogue *rules.Catalogue) map[string]time.Duration {
	windows := make(map[string]time.Duration)
	for _, def := range catalogue.All() {
		windows[def.ID] = time.Duration(def.GroupWindowSecs) * time.Second
	}
	return windows
}

// liveRelayTokens adapts the relay's live token set to the session store's string keys.
func liveRelayTokens(r *relay.Relay) session.LiveTokens {
	return func() []string {
		live := r.ActiveTokens()
		tokens := make([]string, len(live))
		for i, token := range live {
			tokens[i] = string(token)
		}
		return tokens
	}
}

// startSessionSweepLoop runs the stale-session sweep until ctx is cancelled, starting at boot.
// A fresh process holds no relay sessions, so rows left by its predecessor age out of grace.
func startSessionSweepLoop(ctx context.Context, every time.Duration, sweeper *session.Sweeper, logger *slog.Logger) {
	janitor{
		every:  every,
		atBoot: true,
		sweep:  sweeper.Sweep,
		failed: "stale session sweep failed",
		found: func(deleted int) {
			logger.Info("swept stale agent sessions", "count", deleted)
		},
	}.run(ctx, logger)
}
