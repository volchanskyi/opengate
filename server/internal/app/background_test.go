package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
	"github.com/volchanskyi/opengate/server/internal/rules"
	"github.com/volchanskyi/opengate/server/internal/session"
)

// testSchedule is a complete schedule for driving the loops. The numbers it
// holds are the running server's, but nothing here asserts anything about them:
// which cadence ships is the binary's decision, and its own tests hold it.
var testSchedule = BackgroundSchedule{
	Gauges:         5 * time.Second,
	DBSize:         60 * time.Second,
	Investigations: time.Minute,
	Reconcile:      time.Hour,
	SessionSweep:   time.Minute,
	SessionGrace:   5 * time.Minute,
	IncidentSweep:  5 * time.Minute,
}

// sweepRepo is a session.Repository double recording the keep-list each sweep
// passes down. Only DeleteStale is exercised; the embedded interface satisfies
// the rest.
type sweepRepo struct {
	session.Repository
	keep  [][]string
	calls int
}

func (r *sweepRepo) DeleteStale(_ context.Context, _ time.Time, keep []string) (int, error) {
	r.calls++
	r.keep = append(r.keep, keep)
	return len(keep), nil
}

// nopConn is a relay.Conn that never carries traffic — enough to register a
// side and hold the session open.
type nopConn struct{}

func (nopConn) ReadMessage() ([]byte, error) { select {} }
func (nopConn) WriteMessage([]byte) error    { return nil }
func (nopConn) Close() error                 { return nil }

// TestLiveRelayTokens_MirrorsTheRelay pins the keep-list the sweep spares rows
// by. A token the relay holds that this drops would have its session row
// deleted out from under a live connection.
func TestLiveRelayTokens_MirrorsTheRelay(t *testing.T) {
	agentRelay := relay.NewRelay(slog.Default())
	live := liveRelayTokens(agentRelay)
	assert.Empty(t, live())

	token := protocol.GenerateSessionToken()
	require.NoError(t, agentRelay.Register(context.Background(), token, nopConn{}, relay.SideBrowser))

	assert.Equal(t, []string{string(token)}, live())

	agentRelay.Unregister(token)
	assert.Empty(t, live())
}

// TestStartSessionSweepLoop_SweepsAtBootThenStops covers the pass that runs
// before the first tick: a process that just started holds no relay sessions,
// so rows its predecessor left behind are collectable straight away rather than
// one interval later.
func TestStartSessionSweepLoop_SweepsAtBootThenStops(t *testing.T) {
	repo := &sweepRepo{}
	agentRelay := relay.NewRelay(slog.Default())
	sweeper := session.NewSweeper(repo, liveRelayTokens(agentRelay), testSchedule.SessionGrace, slog.Default())

	// A context already cancelled leaves exactly the boot pass observable: the
	// loop sweeps, then finds ctx done instead of waiting out an interval.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startSessionSweepLoop(ctx, testSchedule.SessionSweep, sweeper, slog.Default())

	assert.Equal(t, 1, repo.calls)
	require.Len(t, repo.keep, 1)
	assert.Empty(t, repo.keep[0])
}

// TestAScheduleWithAHoleInItIsRefused covers what a zero duration would
// otherwise do: time.NewTicker panics on it, inside a goroutine nobody is
// watching, and the worker that was supposed to be there simply is not.
func TestAScheduleWithAHoleInItIsRefused(t *testing.T) {
	full := testSchedule
	require.NoError(t, full.Validate())

	missing := full
	missing.IncidentSweep = 0
	err := missing.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IncidentSweep", "the refusal names the field")

	negative := full
	negative.Gauges = -time.Second
	assert.Error(t, negative.Validate(), "a negative interval is a hole too")
}

// quietRoomResolver counts sweeps and reports what it was asked to hold rooms
// open for.
type quietRoomResolver struct {
	calls   int
	windows map[string]time.Duration
}

func (r *quietRoomResolver) ResolveStale(_ context.Context, windows map[string]time.Duration) (int, error) {
	r.calls++
	r.windows = windows
	return 0, nil
}

// TestStartIncidentSweepLoop_SweepsAtBootThenStops covers the pass before the
// first tick. A process that was down for longer than a room's whole hold comes
// back to a triage queue holding incidents that should already have closed, and
// waiting out an interval before looking would leave them there.
func TestStartIncidentSweepLoop_SweepsAtBootThenStops(t *testing.T) {
	resolver := &quietRoomResolver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, resolver,
		map[string]time.Duration{"disk-critical": time.Hour}, slog.Default())

	assert.Equal(t, 1, resolver.calls)
	assert.Equal(t, map[string]time.Duration{"disk-critical": time.Hour}, resolver.windows)
}

// TestGroupWindowsAreTheRulesOwn pins the one number auto-resolve is allowed to
// use. A room must stay open for exactly as long as a new alert could still fold
// into it, so the hold is read from each rule's grouping window rather than
// being a figure of the sweep's own — any other value makes auto-resolve and
// grouping disagree, and a recurrence fragments into a queue of one-offs.
func TestGroupWindowsAreTheRulesOwn(t *testing.T) {
	catalogue, err := rules.Embedded()
	require.NoError(t, err)

	windows := groupWindows(catalogue)

	shipped := catalogue.All()
	require.NotEmpty(t, shipped)
	assert.Len(t, windows, len(shipped), "every shipped rule's rooms must be closeable")
	for _, def := range shipped {
		assert.Equalf(t, time.Duration(def.GroupWindowSecs)*time.Second, windows[def.ID],
			"%s holds its rooms open for its own grouping window", def.ID)
	}
}

// recordingLogger captures what a sweep said, so the contract in the janitor's
// own comment — a failure is always worth a line, a pass that reclaimed nothing
// says nothing — is a test rather than a promise.
func recordingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// countingResolver reports a fixed reclamation, or a failure.
type countingResolver struct {
	reclaimed int
	err       error
}

func (r *countingResolver) ResolveStale(context.Context, map[string]time.Duration) (int, error) {
	return r.reclaimed, r.err
}

// A pass that reclaimed something says so, because that is the only place the
// reclamation is visible: the sweep runs on a goroutine nobody is watching and
// changes rows nobody asked about.
func TestASweepThatReclaimedSomethingSaysSo(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, &countingResolver{reclaimed: 7}, nil, logger)

	assert.Contains(t, said.String(), "auto-resolved quiet incidents")
	assert.Contains(t, said.String(), "count=7")
}

// A pass that reclaimed nothing is the ordinary case, and an ordinary case that
// logs is a log nobody reads.
func TestASweepThatReclaimedNothingSaysNothing(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, &countingResolver{}, nil, logger)

	assert.Empty(t, said.String())
}

// A failure is always worth a line. A sweep that cannot run leaves whatever it
// was reclaiming to accumulate, and silence there is how it accumulates unseen.
func TestASweepThatFailedSaysWhy(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep,
		&countingResolver{err: errors.New("the queue is unreadable")}, nil, logger)

	assert.Contains(t, said.String(), "incident auto-resolve sweep failed")
	assert.Contains(t, said.String(), "the queue is unreadable")
}

// A session sweep that collected rows says how many, for the same reason: the
// rows it removed are ones a technician would otherwise still see as live.
func TestASessionSweepThatCollectedRowsSaysHowMany(t *testing.T) {
	logger, said := recordingLogger()
	agentRelay := relay.NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	require.NoError(t, agentRelay.Register(context.Background(), token, nopConn{}, relay.SideBrowser))

	// The double returns one deletion per token it was told to spare, so a live
	// relay entry is what makes this pass reclaim anything at all.
	sweeper := session.NewSweeper(&sweepRepo{}, liveRelayTokens(agentRelay), testSchedule.SessionGrace, logger)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startSessionSweepLoop(ctx, testSchedule.SessionSweep, sweeper, logger)

	assert.Contains(t, said.String(), "swept stale agent sessions")
	assert.Contains(t, said.String(), "count=1")
}

// countingOrphanSweeper reclaims a fixed number of orphaned series, or fails.
type countingOrphanSweeper struct {
	reclaimed int
	err       error
	// after ends the loop once a pass has been made, so a test sees exactly one.
	after func()
}

func (s *countingOrphanSweeper) Sweep(context.Context) (int, error) {
	if s.after != nil {
		s.after()
	}
	return s.reclaimed, s.err
}

// Orphaned series are the defence in depth behind a purge that partly failed,
// so a pass that found any is a pass that found a failed purge — the one thing
// this sweep exists to surface.
func TestAReconcileSweepThatFoundOrphansWarns(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The sweep ends the loop once it has run, so exactly one pass is observable
	// and the test never waits on a second tick.
	sweeper := &countingOrphanSweeper{reclaimed: 3, after: cancel}
	startReconcileLoop(ctx, time.Millisecond, sweeper, logger)

	assert.Contains(t, said.String(), "reconcile sweep purged orphan telemetry")
	assert.Contains(t, said.String(), "count=3")
}

// It waits out the first interval rather than sweeping at boot: the orphans it
// collects can only be left behind by a purge this process ran, so there is
// nothing waiting for it when it starts.
func TestTheReconcileSweepDoesNotRunAtBoot(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startReconcileLoop(ctx, time.Hour, &countingOrphanSweeper{reclaimed: 3}, logger)

	assert.Empty(t, said.String(), "a cancelled context leaves no pass to have made")
}
