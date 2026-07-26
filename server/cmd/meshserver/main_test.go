package main

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/relay"
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

type relayCleanupRepo struct {
	session.Repository
	token string
	err   error
}

func (r *relayCleanupRepo) DeleteRelaySession(_ context.Context, token string) error {
	r.token = token
	return r.err
}

func TestCleanupRelaySessionUsesBackgroundDelete(t *testing.T) {
	token := protocol.GenerateSessionToken()

	t.Run("deletes by relay token", func(t *testing.T) {
		repo := &relayCleanupRepo{}
		require.NoError(t, cleanupRelaySession(repo, token))
		assert.Equal(t, string(token), repo.token)
	})

	t.Run("propagates repository failure", func(t *testing.T) {
		want := errors.New("delete failed")
		repo := &relayCleanupRepo{err: want}
		assert.ErrorIs(t, cleanupRelaySession(repo, token), want)
	})
}
