package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

// startCounter stands in for dialling. It records how many machines were asked
// for and lets a test end one on demand, so the fleet's bookkeeping is exercised
// without a server on the other end.
type startCounter struct {
	mu       sync.Mutex
	started  int
	stopped  int
	failFrom int
}

func (s *startCounter) start(ctx context.Context, index int) agentResult {
	s.mu.Lock()
	s.started++
	shouldFail := s.failFrom > 0 && index >= s.failFrom
	s.mu.Unlock()

	if shouldFail {
		return agentResult{err: errors.New("dial refused")}
	}

	<-ctx.Done()

	s.mu.Lock()
	s.stopped++
	s.mu.Unlock()
	return agentResult{connectDur: 5 * time.Millisecond, handshakeDur: time.Millisecond}
}

func (s *startCounter) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started, s.stopped
}

// startedCount and stoppedCount are the two halves of counts(), named so a case
// asserting one of them reads as a sentence rather than as an ignored blank.
func (s *startCounter) startedCount() int {
	started, _ := s.counts()
	return started
}

func (s *startCounter) stoppedCount() int {
	_, stopped := s.counts()
	return stopped
}
