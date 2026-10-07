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

func (h *heldStarter) letThemIn() { close(h.arrive) }

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
