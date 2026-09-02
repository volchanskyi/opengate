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

// Everything the assembled product runs on a timer rather than on a request:
// the gauges the platform is watched through, and the janitors that reclaim
// what accumulates while nobody is looking.
//
// They share one loop rather than a copy each, because a sweep that treats an
// error or a cancelled context differently from its neighbours is a difference
// nobody will notice until the day it matters.

// A background janitor: something that accumulates, a pass that reclaims it, and
// a line saying how much. Every periodic sweep in this process has that shape,
// so they share one loop rather than three copies that can drift in how they
// treat an error or a cancelled context.
type janitor struct {
	// every is how often the pass runs.
	every time.Duration
	// atBoot runs a pass before the first tick. A janitor whose backlog is
	// already waiting when the process starts — sessions its predecessor left,
	// incidents that went quiet while it was down — has work to do immediately;
	// one guarding against a failure that can only happen while this process is
	// running has none.
	atBoot bool
	// sweep does one pass and reports how much it reclaimed.
	sweep func(context.Context) (int, error)
	// failed and found are what an operator reads. A failure is always worth a
	// line; a pass that reclaimed nothing is the ordinary case and says nothing.
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

// orphanSweeper reclaims telemetry series no device owns any more. Named here
// as a port so the loop can be driven without a metrics store, the same way the
// incident sweep is.
type orphanSweeper interface {
	Sweep(ctx context.Context) (int, error)
}

// startReconcileLoop runs the reconciliation sweep until ctx is cancelled. It
// waits out the first interval: the orphans it collects can only be left behind
// by a purge this process ran, so there is nothing waiting for it at boot.
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

// staleIncidentResolver closes the rooms whose hold has run out. Named here as a
// port so the loop can be driven without a database.
type staleIncidentResolver interface {
	ResolveStale(ctx context.Context, windows map[string]time.Duration) (int, error)
}

// startIncidentSweepLoop closes the incidents nothing is arriving in any more.
// It starts with one pass at boot, because a process that was down for longer
// than a room's whole hold comes back to a queue holding incidents that should
// already have closed.
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

// expiredRowSweeper reclaims what has been held longer than it is kept for.
// Named here as a port so the loop can be driven without a database.
type expiredRowSweeper interface {
	SweepExpired(ctx context.Context, horizon time.Duration) (int, error)
}

// startRetentionSweepLoop removes the alerts, evidence and closed rooms held
// longer than the horizon. It starts with one pass at boot: these tables only
// grow, and a process that was down comes back to rows that went past the
// horizon while it was gone, on top of whatever the outage itself accumulated.
//
// Unlike the other janitors, this one destroys a customer's records rather than
// reclaiming the system's own leftovers, so a pass that removed anything says
// how much. A pass that removed nothing stays silent, as they all do.
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

// BackgroundSchedule is how often each periodic worker runs, and how long a
// session row outlives the relay that stopped holding its token.
//
// It is a parameter rather than a set of constants here because the cadence is
// the caller's to choose: the binary runs on the production one, and a test
// that has to watch a sweep actually reclaim something cannot wait a minute for
// the next pass. A zero field is refused rather than defaulted — a schedule
// half-filled in is a worker that never runs, and a ticker built from zero
// panics on the spot.
type BackgroundSchedule struct {
	// Gauges is how often the in-memory runtime counts are read.
	Gauges time.Duration
	// DBSize is how often the database's on-disk size is measured.
	DBSize time.Duration
	// Investigations is how often the rule-pack and queue aggregates run.
	Investigations time.Duration
	// Reconcile is how often orphaned telemetry series are swept.
	Reconcile time.Duration
	// SessionSweep is how often session rows the relay no longer holds are
	// reclaimed, and SessionGrace is how long one survives before it can be.
	SessionSweep time.Duration
	SessionGrace time.Duration
	// IncidentSweep is how often rooms whose hold has run out are closed.
	IncidentSweep time.Duration
	// RetentionSweep is how often records past the retention horizon are
	// removed, and RetentionHorizon is how long one is kept before it can be.
	RetentionSweep   time.Duration
	RetentionHorizon time.Duration
}

// Validate refuses a schedule with a hole in it, naming the field. A zero
// duration would panic inside time.NewTicker on a goroutine nobody is watching,
// which surfaces as a worker that silently is not there.
//
// It is exported so the caller that owns the numbers can hold them to this
// without standing the product up.
func (s BackgroundSchedule) Validate() error {
	for name, d := range map[string]time.Duration{
		"Gauges":         s.Gauges,
		"DBSize":         s.DBSize,
		"Investigations": s.Investigations,
		"Reconcile":      s.Reconcile,
		"SessionSweep":   s.SessionSweep,
		"SessionGrace":   s.SessionGrace,
		"IncidentSweep":  s.IncidentSweep,
		// A zero horizon is refused for a second reason: it would put the
		// cutoff at the present instant and delete everything on the first
		// pass, which is a misconfiguration rather than a policy.
		"RetentionSweep":   s.RetentionSweep,
		"RetentionHorizon": s.RetentionHorizon,
	} {
		if d <= 0 {
			return fmt.Errorf("app: BackgroundSchedule.%s must be positive", name)
		}
	}
	return nil
}

// StartBackgroundWorkers launches every periodic worker the assembled product
// runs and returns immediately. Each stops when ctx is cancelled.
//
// Starting them belongs to whoever built the product rather than to the binary,
// so an acceptance test can stand the whole thing up — sweeps included — and
// state a reclamation as an outcome instead of asserting it against the sweep's
// own unit tests and nothing joined.
func (a *Assembly) StartBackgroundWorkers(ctx context.Context, sched BackgroundSchedule) error {
	if err := sched.Validate(); err != nil {
		return err
	}

	go appmetrics.StartGaugeUpdater(ctx, a.Metrics, appmetrics.GaugeSource{
		ActiveSessions:      a.Relay.ActiveSessionCount,
		ConnectedAgents:     a.Agents.ConnectedAgentCount,
		ConnectedMPSDevices: a.AMT.ConnectedDeviceCount,
	}, sched.Gauges)
	go appmetrics.StartDBSizeUpdater(ctx, a.Metrics, a.Store, a.Logger, sched.DBSize)
	go appmetrics.StartDBPoolUpdater(ctx, a.Metrics,
		appmetrics.SQLPoolStatter(a.Store.PoolStats), sched.Gauges)

	// Platform meta-monitoring of the rule pack and the queue it feeds. Both
	// gauges are counts over tables that only grow, so they are refreshed here
	// on a timer rather than computed inside the collector, where a full
	// aggregate would run on every scrape of an endpoint more than one thing
	// scrapes.
	go appmetrics.StartInvestigationsUpdater(ctx, a.Metrics, appmetrics.InvestigationSource{
		OpenInvestigations: a.Alerts.OpenInvestigations,
		FleetRuleCoverage:  a.Agents.FleetRuleCoverage,
	}, a.Logger, sched.Investigations)

	// Garbage-collect any orphaned telemetry series (defense in depth against a
	// purge that partially failed). Purging off leaves nothing to reconcile, and
	// the assembly is where that is known — a nil pointer handed to the loop as
	// a port would arrive there as a non-nil interface and be swept against.
	if a.Reconciler != nil {
		go startReconcileLoop(ctx, sched.Reconcile, a.Reconciler, a.Logger)
	}

	// Garbage-collect session rows the relay no longer holds, so a token that was
	// never connected — or one this process was serving when it last died — stops
	// showing up as an active session.
	sweeper := session.NewSweeper(a.Sessions, liveRelayTokens(a.Relay), sched.SessionGrace, a.Logger)
	go startSessionSweepLoop(ctx, sched.SessionSweep, sweeper, a.Logger)

	// Close the incidents nothing is arriving in any more, so a customer's triage
	// queue holds the problems they still have.
	go startIncidentSweepLoop(ctx, sched.IncidentSweep, a.Alerts, groupWindows(a.Rules), a.Logger)

	// Remove the alerts, evidence and closed rooms held longer than they are
	// kept for, so the retention period the product declares is the one the
	// tables actually observe.
	go startRetentionSweepLoop(ctx, sched.RetentionSweep, sched.RetentionHorizon, a.Alerts, a.Logger)

	// Sync agent manifests from GitHub releases (default: every hour).
	if a.githubRepo != "" {
		go updater.StartPeriodicSync(ctx, a.githubRepo, 0, a.SigningKeys, a.Manifests, a.Logger)
	}
	return nil
}

// groupWindows is how long each shipped rule's room stays open with nothing
// further arriving. It is the rule's own grouping window and never a figure of
// its own: a room must stay open for exactly as long as a new alert could still
// fold into it, or auto-resolve and grouping disagree and a recurrence
// fragments into a queue of one-offs.
func groupWindows(catalogue *rules.Catalogue) map[string]time.Duration {
	windows := make(map[string]time.Duration)
	for _, def := range catalogue.All() {
		windows[def.ID] = time.Duration(def.GroupWindowSecs) * time.Second
	}
	return windows
}

// liveRelayTokens adapts the relay's live token set to the plain strings the
// session store keys on.
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

// startSessionSweepLoop runs the stale-session sweep until ctx is cancelled,
// starting with one pass at boot: a process that just started holds no relay
// sessions, so any row left behind by its predecessor is collectable as soon as
// it ages past the grace period.
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
