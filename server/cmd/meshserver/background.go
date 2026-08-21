package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/volchanskyi/opengate/server/internal/agentapi"
	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/amt"
	"github.com/volchanskyi/opengate/server/internal/db"
	"github.com/volchanskyi/opengate/server/internal/lifecycle"
	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
	"github.com/volchanskyi/opengate/server/internal/signaling"
	"github.com/volchanskyi/opengate/server/internal/updater"
)

// Everything this process runs on a timer rather than on a request: the gauges
// the platform is watched through, and the janitors that reclaim what
// accumulates while nobody is looking.
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

// reconcileInterval is how often the orphan-series sweep runs.
const reconcileInterval = time.Hour

// startReconcileLoop runs the reconciliation sweep until ctx is cancelled. A nil
// reconciler (purging disabled) returns immediately. It waits out the first
// interval: the orphans it collects can only be left behind by a purge this
// process ran, so there is nothing waiting for it at boot.
func startReconcileLoop(ctx context.Context, reconciler *lifecycle.Reconciler, logger *slog.Logger) {
	if reconciler == nil {
		return
	}
	janitor{
		every:  reconcileInterval,
		sweep:  reconciler.Sweep,
		failed: "reconcile sweep failed",
		found: func(purged int) {
			logger.Warn("reconcile sweep purged orphan telemetry", "count", purged)
		},
	}.run(ctx, logger)
}

// incidentSweepInterval is how often the auto-resolve sweep runs. It decides
// only how promptly a room that has gone quiet leaves the triage queue, not
// whether it does: the hold itself is the rule's own grouping window, and an
// alert arriving after that window closes the lapsed room on its way past.
const incidentSweepInterval = 5 * time.Minute

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
	ctx context.Context, store staleIncidentResolver, windows map[string]time.Duration, logger *slog.Logger,
) {
	janitor{
		every:  incidentSweepInterval,
		atBoot: true,
		sweep:  func(ctx context.Context) (int, error) { return store.ResolveStale(ctx, windows) },
		failed: "incident auto-resolve sweep failed",
		found: func(resolved int) {
			logger.Info("auto-resolved quiet incidents", "count", resolved)
		},
	}.run(ctx, logger)
}

// backgroundLoops is everything this process runs on a timer rather than on a
// request: the gauges the platform is watched through, and the janitors that
// reclaim what accumulates while nobody is looking.
type backgroundLoops struct {
	metrics     *appmetrics.Metrics
	store       *db.PostgresStore
	relay       *relay.Relay
	agents      *agentapi.AgentServer
	amt         *amt.Service
	signaling   *signaling.Tracker
	alerts      *alerts.Store
	sessions    session.Repository
	reconciler  *lifecycle.Reconciler
	catalogue   *rules.Catalogue
	signingKeys *updater.SigningKeys
	manifests   *updater.ManifestStore
	githubRepo  string
	logger      *slog.Logger
}

// startBackgroundLoops launches every periodic worker and returns immediately.
// Each stops when ctx is cancelled.
func startBackgroundLoops(ctx context.Context, d backgroundLoops) {
	go appmetrics.StartGaugeUpdater(ctx, d.metrics, appmetrics.GaugeSource{
		ActiveSessions:      d.relay.ActiveSessionCount,
		ConnectedAgents:     d.agents.ConnectedAgentCount,
		ConnectedMPSDevices: d.amt.ConnectedDeviceCount,
		SignalingSuccesses:  d.signaling.SuccessCount,
		SignalingFailures:   d.signaling.FailureCount,
	}, gaugeRefreshInterval)
	go appmetrics.StartDBSizeUpdater(ctx, d.metrics, d.store, d.logger, dbSizeRefreshInterval)

	// Platform meta-monitoring of the rule pack and the queue it feeds. Both
	// gauges are counts over tables that only grow, so they are refreshed here
	// on a timer rather than computed inside the collector, where a full
	// aggregate would run on every scrape of an endpoint more than one thing
	// scrapes.
	go appmetrics.StartInvestigationsUpdater(ctx, d.metrics, appmetrics.InvestigationSource{
		OpenInvestigations: d.alerts.OpenInvestigations,
		FleetRuleCoverage:  d.agents.FleetRuleCoverage,
	}, d.logger, investigationsRefreshInterval)

	// Garbage-collect any orphaned telemetry series (defense in depth against a
	// purge that partially failed). A no-op when purging is disabled.
	go startReconcileLoop(ctx, d.reconciler, d.logger)

	// Garbage-collect session rows the relay no longer holds, so a token that was
	// never connected — or one this process was serving when it last died — stops
	// showing up as an active session.
	sweeper := session.NewSweeper(d.sessions, liveRelayTokens(d.relay), sessionGracePeriod, d.logger)
	go startSessionSweepLoop(ctx, sweeper, d.logger)

	// Close the incidents nothing is arriving in any more, so a customer's triage
	// queue holds the problems they still have.
	go startIncidentSweepLoop(ctx, d.alerts, groupWindows(d.catalogue), d.logger)

	// Sync agent manifests from GitHub releases (default: every hour).
	if d.githubRepo != "" {
		go updater.StartPeriodicSync(ctx, d.githubRepo, 0, d.signingKeys, d.manifests, d.logger)
	}
}

// gaugeRefreshInterval is how often the runtime gauges — sessions, connected
// agents, connected AMT devices, signaling — are read from the components that
// hold them. They are in-memory counts, so this is cheap and close to the scrape.
const gaugeRefreshInterval = 15 * time.Second

// dbSizeRefreshInterval is how often the database's on-disk size is measured. It
// moves slowly and the query is not free, so it is read far less often than it
// is scraped.
const dbSizeRefreshInterval = 60 * time.Second

// investigationsRefreshInterval is how often the platform's own view of the
// investigation tables is recomputed. It is deliberately slower than the scrape:
// these are aggregates over tables that only grow, and a triage queue moves at
// the speed of people rather than of requests, so a minute-old count answers
// every question the gauges are asked.
const investigationsRefreshInterval = time.Minute

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

// sessionGracePeriod is how long a session row survives without the relay
// holding its token. It only has to outlast the gap between issuing a token and
// connecting with it — a live session is spared by the keep-list however old it
// gets — so it doubles as the worst-case lag before a session orphaned by a
// process restart disappears from the device page.
const sessionGracePeriod = 5 * time.Minute

// sessionSweepInterval is how often the stale-session sweep runs.
const sessionSweepInterval = time.Minute

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
func startSessionSweepLoop(ctx context.Context, sweeper *session.Sweeper, logger *slog.Logger) {
	janitor{
		every:  sessionSweepInterval,
		atBoot: true,
		sweep:  sweeper.Sweep,
		failed: "stale session sweep failed",
		found: func(deleted int) {
			logger.Info("swept stale agent sessions", "count", deleted)
		},
	}.run(ctx, logger)
}
