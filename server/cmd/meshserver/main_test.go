package main

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
	sweeper := session.NewSweeper(repo, liveRelayTokens(agentRelay), sessionGracePeriod, slog.Default())

	// A context already cancelled leaves exactly the boot pass observable: the
	// loop sweeps, then finds ctx done instead of waiting out an interval.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	startSessionSweepLoop(ctx, sweeper, slog.Default())

	assert.Equal(t, 1, repo.calls)
	require.Len(t, repo.keep, 1)
	assert.Empty(t, repo.keep[0])
}

// TestSessionSweepTiming keeps the sweep frequent relative to the grace period,
// so an orphaned row never lingers materially past that window.
func TestSessionSweepTiming(t *testing.T) {
	assert.Positive(t, sessionSweepInterval)
	assert.Less(t, sessionSweepInterval, sessionGracePeriod)
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

	startIncidentSweepLoop(ctx, resolver, map[string]time.Duration{"disk-critical": time.Hour}, slog.Default())

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

// TestInvestigationsRefreshIsSlowerThanTheScrape states the property that keeps
// the gauges off the request path. The refresh is what reads the database; the
// scrape only reads what it left behind, so the two are deliberately not the
// same rate.
func TestInvestigationsRefreshIsSlowerThanTheScrape(t *testing.T) {
	assert.GreaterOrEqual(t, investigationsRefreshInterval, 30*time.Second,
		"a count over tables that only grow is not recomputed at scrape speed")
}
