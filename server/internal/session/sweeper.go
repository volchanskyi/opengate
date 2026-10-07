package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// LiveTokens reports the session tokens that currently hold a relay connection.
type LiveTokens func() []string

// Sweeper garbage-collects agent session rows that outlived their relay.
// A row is stale only when it is older than the grace period and the relay holds no token for it.
type Sweeper struct {
	repo   Repository
	live   LiveTokens
	grace  time.Duration
	logger *slog.Logger
}

// NewSweeper builds a stale-session sweep that spares unclaimed rows for grace.
// A nil logger uses slog.Default.
func NewSweeper(repo Repository, live LiveTokens, grace time.Duration, logger *slog.Logger) *Sweeper {
	if logger == nil {
		logger = slog.Default()
	}
	return &Sweeper{repo: repo, live: live, grace: grace, logger: logger}
}

// Sweep deletes every session row past the grace period whose token the relay does not hold.
// It returns how many rows it removed.
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	deleted, err := s.repo.DeleteStale(ctx, time.Now().Add(-s.grace), s.live())
	if err != nil {
		return 0, fmt.Errorf("sweep stale sessions: %w", err)
	}
	return deleted, nil
}
