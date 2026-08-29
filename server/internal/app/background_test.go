package app

import (
	"context"
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
