package app

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// testSchedule is a complete schedule for driving the loops.
var testSchedule = BackgroundSchedule{
	Gauges:         5 * time.Second,
	DBSize:         60 * time.Second,
	Investigations: time.Minute,
	Reconcile:      time.Hour,
	SessionSweep:   time.Minute,
	SessionGrace:   5 * time.Minute,
	IncidentSweep:  5 * time.Minute,

	RetentionSweep:   6 * time.Hour,
	RetentionHorizon: 365 * 24 * time.Hour,
}

// sweepRepo is a session.Repository double recording the keep-list each sweep passes down.
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

// nopConn is a relay.Conn that carries no traffic.
type nopConn struct{}

func (nopConn) ReadMessage() ([]byte, error) { select {} }
func (nopConn) WriteMessage([]byte) error    { return nil }
func (nopConn) Close() error                 { return nil }

func TestLiveRelayTokens_MirrorsTheRelay(t *testing.T) {
	agentRelay := relay.NewRelay(slog.Default())
	live := liveRelayTokens(agentRelay)
	assert.Empty(t, live())

	token := protocol.GenerateSessionToken()
	_, err := agentRelay.Register(context.Background(), token, nopConn{}, relay.SideBrowser)
	require.NoError(t, err)

	assert.Equal(t, []string{string(token)}, live())

	agentRelay.Unregister(token)
	assert.Empty(t, live())
}

func TestStartSessionSweepLoop_SweepsAtBootThenStops(t *testing.T) {
	repo := &sweepRepo{}
	agentRelay := relay.NewRelay(slog.Default())
	sweeper := session.NewSweeper(repo, liveRelayTokens(agentRelay), testSchedule.SessionGrace, slog.Default())

	// A cancelled context leaves exactly the boot pass observable.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startSessionSweepLoop(ctx, testSchedule.SessionSweep, sweeper, slog.Default())

	assert.Equal(t, 1, repo.calls)
	require.Len(t, repo.keep, 1)
	assert.Empty(t, repo.keep[0])
}

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

// quietRoomResolver counts sweeps and records the hold windows it was given.
type quietRoomResolver struct {
	calls   int
	windows map[string]time.Duration
}

func (r *quietRoomResolver) ResolveStale(_ context.Context, windows map[string]time.Duration) (int, error) {
	r.calls++
	r.windows = windows
	return 0, nil
}

func TestStartIncidentSweepLoop_SweepsAtBootThenStops(t *testing.T) {
	resolver := &quietRoomResolver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, resolver,
		map[string]time.Duration{"disk-critical": time.Hour}, slog.Default())

	assert.Equal(t, 1, resolver.calls)
	assert.Equal(t, map[string]time.Duration{"disk-critical": time.Hour}, resolver.windows)
}

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

// recordingLogger captures what a sweep logged.
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

func TestASweepThatReclaimedSomethingSaysSo(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, &countingResolver{reclaimed: 7}, nil, logger)

	assert.Contains(t, said.String(), "auto-resolved quiet incidents")
	assert.Contains(t, said.String(), "count=7")
}

func TestASweepThatReclaimedNothingSaysNothing(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep, &countingResolver{}, nil, logger)

	assert.Empty(t, said.String())
}

func TestASweepThatFailedSaysWhy(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startIncidentSweepLoop(ctx, testSchedule.IncidentSweep,
		&countingResolver{err: errors.New("the queue is unreadable")}, nil, logger)

	assert.Contains(t, said.String(), "incident auto-resolve sweep failed")
	assert.Contains(t, said.String(), "the queue is unreadable")
}

func TestASessionSweepThatCollectedRowsSaysHowMany(t *testing.T) {
	logger, said := recordingLogger()
	agentRelay := relay.NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	_, err := agentRelay.Register(context.Background(), token, nopConn{}, relay.SideBrowser)
	require.NoError(t, err)

	// The double returns one deletion per spared token, so a live relay entry makes the pass reclaim.
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

func TestAReconcileSweepThatFoundOrphansWarns(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The sweep ends the loop once it has run, so exactly one pass is observable.
	sweeper := &countingOrphanSweeper{reclaimed: 3, after: cancel}
	startReconcileLoop(ctx, time.Millisecond, sweeper, logger)

	assert.Contains(t, said.String(), "reconcile sweep purged orphan telemetry")
	assert.Contains(t, said.String(), "count=3")
}

func TestTheReconcileSweepDoesNotRunAtBoot(t *testing.T) {
	logger, said := recordingLogger()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startReconcileLoop(ctx, time.Hour, &countingOrphanSweeper{reclaimed: 3}, logger)

	assert.Empty(t, said.String(), "a cancelled context leaves no pass to have made")
}

// expiredRowCollector counts sweeps and reports the horizon it was handed.
type expiredRowCollector struct {
	calls   int
	horizon time.Duration
	err     error
}

func (c *expiredRowCollector) SweepExpired(_ context.Context, horizon time.Duration) (int, error) {
	c.calls++
	c.horizon = horizon
	return 0, c.err
}

func TestStartRetentionSweepLoop_SweepsAtBootThenStops(t *testing.T) {
	collector := &expiredRowCollector{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	startRetentionSweepLoop(ctx, testSchedule.RetentionSweep, testSchedule.RetentionHorizon,
		collector, slog.New(slog.NewTextHandler(io.Discard, nil)))

	assert.Equal(t, 1, collector.calls, "the sweep runs once at boot, then the cancelled context stops it")
	assert.Equal(t, testSchedule.RetentionHorizon, collector.horizon,
		"the loop passes the configured horizon down, never one of its own")
}

func TestARetentionSweepThatReclaimedSomething(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reclaimed int
		wantLog   bool
	}{
		{"something", 42, true},
		{"nothing", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, nil))
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			janitor{
				every:  time.Hour,
				atBoot: true,
				sweep:  func(context.Context) (int, error) { return tc.reclaimed, nil },
				failed: "retention sweep failed",
				found: func(removed int) {
					logger.Info("reclaimed records past the retention horizon", "count", removed)
				},
			}.run(ctx, logger)

			if tc.wantLog {
				assert.Contains(t, buf.String(), "retention horizon")
				assert.Contains(t, buf.String(), "count=42")
				return
			}
			assert.Empty(t, buf.String())
		})
	}
}
