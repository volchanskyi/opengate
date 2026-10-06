package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// aCancelledStart is a machine whose start ends because the run stood it down
// before it ever registered.
func aCancelledStart(ctx context.Context) agentResult {
	<-ctx.Done()
	return agentResult{err: fmt.Errorf("enroll soak-t0-a1: %w", ctx.Err())}
}

func TestAMachineTheRunStoodDownIsNotCountedAsAFailureToArrive(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, _ fleetPresence) agentResult {
		return aCancelledStart(ctx)
	})

	require.NoError(t, fleet.HoldConnected(0, 3))
	fleet.Stop()

	outcomes := fleet.Outcomes()
	assert.Zero(t, outcomes.Failed, "the run cancelled these, so nothing failed to arrive")
	assert.Equal(t, int64(3), outcomes.StoodDown, "and the run says how many it stood down")
	assert.Zero(t, outcomes.ErrorRate(), "a phase of nothing but wind-down has no error rate")
}

func TestAMachineTheServerWouldNotTakeIsStillAFailure(t *testing.T) {
	fleet := NewQUICFleet(func(_ context.Context, index int, presence fleetPresence) agentResult {
		if index == 0 {
			return agentResult{err: fmt.Errorf("enroll: context deadline exceeded")}
		}
		presence.Arrived()
		return agentResult{}
	})

	require.NoError(t, fleet.HoldConnected(0, 2))
	fleet.Stop()

	outcomes := fleet.Outcomes()
	assert.Equal(t, int64(1), outcomes.Failed)
	assert.Zero(t, outcomes.StoodDown)
}

func TestAnArrivedMachineStoodDownIsCountedAsArrived(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, presence fleetPresence) agentResult {
		presence.Arrived()
		<-ctx.Done()
		presence.Left()
		return agentResult{err: ctx.Err()}
	})

	require.NoError(t, fleet.HoldConnected(0, 2))
	require.Eventually(t, func() bool { return fleet.Outcomes().Arrived == 2 },
		2*time.Second, 10*time.Millisecond)
	fleet.Stop()

	outcomes := fleet.Outcomes()
	assert.Equal(t, int64(2), outcomes.Arrived)
	assert.Zero(t, outcomes.Failed)
	assert.Zero(t, outcomes.StoodDown, "it arrived, so it is not one the run never got")
}

func TestTheResultsBlockHoldsStoodDownMachinesApartFromFailures(t *testing.T) {
	results := []agentResult{
		{arrivedAt: time.Now()},
		{err: fmt.Errorf("enroll soak-t0-a1: %w", context.Canceled)},
		{err: fmt.Errorf("dial: %w", context.Canceled)},
		{err: fmt.Errorf("enroll soak-t0-a3: context deadline exceeded")},
	}

	var failures int
	printed := captureStdout(t, func() {
		failures = reportResults(results, time.Now().Add(-time.Minute), time.Minute, 4, nil)
	})

	assert.Equal(t, 1, failures, "only the machine the server would not take failed")
	assert.Contains(t, printed, "Agents:      1/4 succeeded")
	assert.Contains(t, printed, "Stood down:  2")
}

func TestTheResultsBlockCountsAMachineSeveredUnderLoad(t *testing.T) {
	results := []agentResult{
		{connectDur: 5 * time.Millisecond, arrivedAt: time.Now()},
		{connectDur: 200 * time.Millisecond, arrivedAt: time.Now(), err: ErrHeldPeerGone},
		{err: fmt.Errorf("enroll soak-t0-a3: context deadline exceeded")},
	}

	var failures int
	printed := captureStdout(t, func() {
		failures = reportResults(results, time.Now().Add(-time.Minute), time.Minute, 3, nil)
	})

	assert.Equal(t, 1, failures, "only the machine that never got in failed to arrive")
	assert.Contains(t, printed, "Agents:      2/3 succeeded")
	assert.Contains(t, printed, "p99=200ms",
		"the severed machine's connect time is one of the run's connect times")
}
