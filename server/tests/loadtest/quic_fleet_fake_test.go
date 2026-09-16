package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

// awaitConnected waits for the fleet to report the level asked for.
//
// It is a wait rather than a read because the two are different moments: the
// level is true the instant it is asked for, and a machine is one of the
// connected only once it has arrived — which is the whole of what the fleet's
// own count says and the level does not.
func awaitConnected(t *testing.T, fleet *QUICFleet, want int) {
	t.Helper()
	require.Eventually(t, func() bool { return fleet.Connected() == want },
		2*time.Second, 10*time.Millisecond)
}

// awaitFailures waits for the fleet to have recorded that many machines never
// arrived.
//
// It is its own wait because the fleet's count of the connected no longer
// answers it. A machine is one of the connected once it has arrived, so that
// count reaches its level as soon as the arrivals land — whether or not the
// machines that were refused have finished reporting. Reading `len(running)`
// made the two one fact, and a test that waited for the count was implicitly
// waiting for the refusals as well.
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
