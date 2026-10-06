package main

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type startCounter struct {
	mu       sync.Mutex
	started  int
	stopped  int
	failFrom int
}

func (s *startCounter) start(ctx context.Context, index int, presence fleetPresence) agentResult {
	s.mu.Lock()
	s.started++
	shouldFail := s.failFrom > 0 && index >= s.failFrom
	s.mu.Unlock()

	if shouldFail {
		return agentResult{err: errors.New("dial refused")}
	}

	presence.Arrived()
	<-ctx.Done()
	presence.Left()

	s.mu.Lock()
	s.stopped++
	s.mu.Unlock()
	return agentResult{connectDur: 5 * time.Millisecond, handshakeDur: time.Millisecond}
}

type liveIndexes struct {
	mu   sync.Mutex
	live map[int]bool
}

func (l *liveIndexes) start(ctx context.Context, index int, presence fleetPresence) agentResult {
	l.mu.Lock()
	if l.live == nil {
		l.live = map[int]bool{}
	}
	l.live[index] = true
	l.mu.Unlock()

	presence.Arrived()
	<-ctx.Done()
	presence.Left()

	l.mu.Lock()
	delete(l.live, index)
	l.mu.Unlock()
	return agentResult{connectDur: 5 * time.Millisecond, handshakeDur: time.Millisecond}
}

func (l *liveIndexes) indexes() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]int, 0, len(l.live))
	for index := range l.live {
		out = append(out, index)
	}
	sort.Ints(out)
	return out
}

func awaitConnected(t *testing.T, fleet *QUICFleet, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return fleet.Connected() == want },
		2*time.Second, 10*time.Millisecond)
}

func awaitFailures(t *testing.T, fleet *QUICFleet, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(fleet.Failures()) == want },
		2*time.Second, 10*time.Millisecond)
}

func (s *startCounter) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started, s.stopped
}

func (s *startCounter) startedCount() int {
	started, _ := s.counts()
	return started
}

func (s *startCounter) stoppedCount() int {
	_, stopped := s.counts()
	return stopped
}
