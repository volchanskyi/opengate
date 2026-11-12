package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dialledAgo starts machines that report a dial made the given time before they register.
type dialledAgo struct {
	ago    time.Duration
	failAt map[int]bool
}

func (d dialledAgo) start(ctx context.Context, index int, presence fleetPresence) agentResult {
	if d.failAt[index] {
		return agentResult{err: errors.New("dial refused")}
	}
	presence.Arrived(time.Now().Add(-d.ago))
	<-ctx.Done()
	presence.Left()
	return agentResult{}
}

// settledWave waits for the wave between from and to to have nobody left dialling.
func settledWave(t *testing.T, fleet *QUICFleet, from, to int) Wave {
	t.Helper()
	var wave Wave
	require.Eventually(t, func() bool {
		wave = fleet.Wave(from, to)
		return wave.Pending == 0
	}, 5*time.Second, 5*time.Millisecond)
	return wave
}

func TestAWaveTimesEachMachineFromItsDialToOnline(t *testing.T) {
	t.Parallel()

	fleet := NewQUICFleet(dialledAgo{ago: 2 * time.Second}.start)
	defer fleet.Stop()

	asked := time.Now()
	require.NoError(t, fleet.HoldConnected(0, 3))
	wave := settledWave(t, fleet, 0, 3)
	require.Len(t, wave.OnlineAt, 3, "each machine online says when it came online")
	for _, at := range wave.OnlineAt {
		assert.False(t, at.Before(asked), "a machine comes online after it was asked for")
		assert.WithinDuration(t, time.Now(), at, 5*time.Second)
	}

	assert.Equal(t, 3, wave.Dialled)
	require.Len(t, wave.Online, 3, "every machine dialled came online")
	for _, online := range wave.Online {
		assert.InDelta(t, float64(2*time.Second), float64(online), float64(500*time.Millisecond),
			"each is timed from the dial it reported")
	}
}

func TestAMachineWhoseDialEndsUnregisteredFinishesItsWaveOffline(t *testing.T) {
	t.Parallel()

	fleet := NewQUICFleet(dialledAgo{failAt: map[int]bool{1: true}}.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 3))
	wave := settledWave(t, fleet, 0, 3)

	assert.Equal(t, 3, wave.Dialled, "the refused machine was dialled")
	assert.Len(t, wave.Online, 2, "and never came online")
}

func TestAMachineStillDiallingKeepsItsWavePending(t *testing.T) {
	t.Parallel()

	starter := newHeldStarter()
	fleet := NewQUICFleet(starter.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 2))
	require.Eventually(t, func() bool { return fleet.Wave(0, 2).Dialled == 2 },
		5*time.Second, 5*time.Millisecond)
	assert.Equal(t, 2, fleet.Wave(0, 2).Pending, "two machines dialling have not finished")

	starter.letThemIn()
	assert.Len(t, settledWave(t, fleet, 0, 2).Online, 2)
}

func TestAWaveReadsOnlyTheMachinesOfItsOwnStretch(t *testing.T) {
	t.Parallel()

	fleet := NewQUICFleet(dialledAgo{ago: time.Second}.start)
	defer fleet.Stop()

	require.NoError(t, fleet.HoldConnected(0, 2))
	settledWave(t, fleet, 0, 2)
	first := fleet.Asked()
	require.NoError(t, fleet.HoldConnected(0, 5))

	assert.Equal(t, 2, first, "the mark after the first climb is the two asked for")
	assert.Equal(t, 5, fleet.Asked())
	wave := settledWave(t, fleet, first, fleet.Asked())
	assert.Equal(t, 3, wave.Dialled, "the second climb's wave is its own three machines")
}

func TestAMachineWoundDownBeforeItsTurnIsNoPartOfTheWave(t *testing.T) {
	t.Parallel()

	fleet := NewQUICFleet(dialledAgo{}.start)
	defer fleet.Stop()

	// The window spaces the two dials an hour apart, so both are still waiting for their turn.
	require.NoError(t, fleet.HoldConnected(2*time.Hour, 2))
	require.NoError(t, fleet.HoldConnected(0, 0))

	wave := settledWave(t, fleet, 0, 2)
	assert.Zero(t, wave.Dialled, "a machine that never dialled is no machine of the wave")
	assert.Empty(t, wave.Online)
	assert.Equal(t, 2, fleet.Asked(), "both were still asked for")
}
