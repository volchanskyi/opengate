package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the fleet reports as connected is the machines that arrived and have not
// ended — not the machines the run has queued to dial.
//
// A phase publishes that number as the level it achieved, and a check beside it
// puts the server's own count of the same machines against it. So a machine that
// has been asked for but has not yet connected, handshook and registered is a
// machine the server cannot see, and counting it here reports a level nobody
// held. On a quarter-processor target where registering took eight seconds, the
// run claimed two thousand machines and a hundred and thirty-seven of them had
// not arrived.

// heldStarter is a machine whose arrival waits for the test to let it in, so the
// gap between being asked for and being there can be looked at.
type heldStarter struct {
	arrive chan struct{}

	mu      sync.Mutex
	started int
	failAt  map[int]bool
}

func newHeldStarter() *heldStarter {
	return &heldStarter{arrive: make(chan struct{}), failAt: map[int]bool{}}
}

func (h *heldStarter) start(ctx context.Context, index int, presence fleetPresence) agentResult {
	h.mu.Lock()
	h.started++
	fails := h.failAt[index]
	h.mu.Unlock()

	if fails {
		return agentResult{err: errors.New("dial refused")}
	}

	select {
	case <-h.arrive:
	case <-ctx.Done():
		return agentResult{err: ctx.Err()}
	}

	presence.Arrived()
	<-ctx.Done()
	presence.Left()
	return agentResult{connectDur: time.Millisecond}
}

// letThemIn releases every machine waiting to arrive.
func (h *heldStarter) letThemIn() { close(h.arrive) }

// A machine the run has asked for is not a machine the server is holding, and
// the fleet says so until it arrives.
func TestAMachineAskedForIsNotConnectedUntilItArrives(t *testing.T) {
	t.Parallel()

	starter := newHeldStarter()
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 5))
	assert.Equal(t, 0, fleet.Connected(),
		"five machines queued to dial are five machines nothing is holding yet")

	starter.letThemIn()
	awaitConnected(t, fleet, 5)
}

// A machine that could not arrive was never connected, whatever the run asked
// for.
func TestAMachineThatCouldNotArriveIsNeverConnected(t *testing.T) {
	t.Parallel()

	starter := newHeldStarter()
	starter.failAt = map[int]bool{2: true, 3: true}
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 4))
	starter.letThemIn()

	awaitConnected(t, fleet, 2)
	assert.Equal(t, 2, fleet.Connected(), "two of four arrived, so the fleet is two")
}

// A machine whose life has ended is not one of the connected, and the fleet
// counts its departure — which is what says how much of the difference between
// two counts of one population is the population changing underneath them.
func TestADepartureLeavesTheConnectedAndIsCounted(t *testing.T) {
	t.Parallel()

	starter := newHeldStarter()
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 3))
	starter.letThemIn()
	awaitConnected(t, fleet, 3)
	assert.Equal(t, int64(0), fleet.Outcomes().Departed, "nothing has left yet")

	require.NoError(t, fleet.HoldConnected(0, 1))
	awaitConnected(t, fleet, 1)
	assert.Equal(t, int64(2), fleet.Outcomes().Departed,
		"two machines that had arrived ended, so two departed")
}

// A machine the run stood down before it ever arrived did not depart: it was
// never there, and counting it would make the wind-down look like churn.
func TestAMachineStoodDownBeforeArrivingDidNotDepart(t *testing.T) {
	t.Parallel()

	starter := newHeldStarter()
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 4))
	require.NoError(t, fleet.HoldConnected(0, 0))

	require.Eventually(t, func() bool { return fleet.Outcomes().StoodDown == 4 },
		2*time.Second, 5*time.Millisecond)
	assert.Equal(t, int64(0), fleet.Outcomes().Departed,
		"nothing that never arrived can have departed")
	assert.Equal(t, 0, fleet.Connected())
}

// A machine whose connection broke is not one of the connected while it is
// away, and is again once it is back.
//
// This is the half that a count of "arrived and not finished" cannot state. A
// machine that flapped was counted for the whole of its absence, so a phase
// published a level that included machines attached to nothing — and the
// server, counting what was actually attached, disagreed by exactly them. On a
// quarter-processor target holding two thousand machines it was 46 of them, and
// on the volume family's eight thousand it was 57.
//
// The arrival tally is the other number and does not move: a machine that came
// back is the same machine returning, so the run's count of arrivals stays one.
func TestAMachineAwayFromItsConnectionIsNotOneOfTheConnected(t *testing.T) {
	t.Parallel()

	away := make(chan struct{})
	back := make(chan struct{})
	fleet := NewQUICFleet(func(ctx context.Context, _ int, presence fleetPresence) agentResult {
		presence.Arrived()
		<-away
		presence.Left()
		<-back
		presence.Arrived()
		<-ctx.Done()
		return agentResult{connectDur: time.Millisecond}
	})
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 1))
	awaitConnected(t, fleet, 1)
	assert.Equal(t, int64(1), fleet.Outcomes().Arrived)

	close(away)
	awaitConnected(t, fleet, 0)
	assert.Equal(t, int64(1), fleet.Outcomes().Arrived,
		"a machine that lost its connection did not un-arrive")
	assert.Equal(t, int64(1), fleet.Outcomes().Departed,
		"the connection that ended is what the census window allows for")

	close(back)
	awaitConnected(t, fleet, 1)
	assert.Equal(t, int64(1), fleet.Outcomes().Arrived,
		"a machine that came back is the same machine returning, counted once")
}
