package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// LiveTokens reports the session tokens that currently hold a relay connection.
// The relay satisfies this; the sweep treats every token it names as in use.
type LiveTokens func() []string

// Sweeper garbage-collects agent session rows that outlived their relay.
//
// A row is deleted by the relay the moment a paired session ends, which covers
// the ordinary case. Two paths escape it: a session that no side ever connected
// to (the browser closed the tab between asking for a token and using it), and
// every session a process was holding when it died. Both leave a row that no
// connection will ever reclaim, and the device page reads those rows back as
// "Active Sessions".
//
// The sweep resolves it without a heartbeat column: a row is stale only if it
// is older than the grace period *and* the relay does not currently hold its
// token, so a long-running desktop session is never collected while a token
// abandoned seconds after issue is collected on the next tick.
type Sweeper struct {
	repo   Repository
	live   LiveTokens
	grace  time.Duration
	logger *slog.Logger
}

// NewSweeper builds a stale-session sweep. grace is how long an unclaimed row
// is spared — long enough to cover a slow page load between issuing a token and
// connecting with it. A nil logger uses slog.Default.
func NewSweeper(repo Repository, live LiveTokens, grace time.Duration, logger *slog.Logger) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{repo: repo, live: live, grace: grace, logger: logger}
}

// Sweep deletes every session row past the grace period whose token the relay
// no longer holds, and returns how many it removed. It is idempotent: a second
// run over a clean store deletes nothing.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	deleted, err := s.repo.DeleteStale(ctx, time.Now().Add(-s.grace), s.live())
	if err != nil {
		return 0, fmt.Errorf("sweep stale sessions: %w", err)
	}
	return deleted, nil
}
