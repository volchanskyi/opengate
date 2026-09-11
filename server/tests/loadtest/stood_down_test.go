package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A machine the run stood down is not a machine that failed to arrive.
//
// A walk winds its level down between phases and again at the end, and the
// starts still reaching for the server when it does are cancelled by the run
// itself. Counted as failures to arrive they are indistinguishable from a
// server that would not take them, and they land in whichever phase the
// wind-down happened in — which for a recovery phase is every outcome it has,
// because a phase that offers no arrivals records nothing else.
//
// It is the same rule the ramp already states about a machine let go before its
// turn, applied where the tally is kept. The first night the fleet arrived, it
// cost four legs of the performance stack and invalidated a fifth: every
// failure in all of them was this, and the one genuine timeout among them was
// the only reading anybody wanted.

// aCancelledStart is a machine whose start ends because the run stood it down
// before it ever registered.
func aCancelledStart(ctx context.Context) agentResult {
	<-ctx.Done()
	return agentResult{err: fmt.Errorf("enroll soak-t0-a1: %w", ctx.Err())}
}

func TestAMachineTheRunStoodDownIsNotCountedAsAFailureToArrive(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, _ func()) agentResult {
		return aCancelledStart(ctx)
	})

	require.NoError(t, fleet.HoldConnected(0, 3))
	fleet.Stop()

	outcomes := fleet.Outcomes()
	assert.Zero(t, outcomes.Failed, "the run cancelled these, so nothing failed to arrive")
	assert.Equal(t, int64(3), outcomes.StoodDown, "and the run says how many it stood down")
	assert.Zero(t, outcomes.ErrorRate(), "a phase of nothing but wind-down has no error rate")
}

// A machine the server would not take is still a failure, and a wind-down
// happening around it does not launder it.
func TestAMachineTheServerWouldNotTakeIsStillAFailure(t *testing.T) {
	fleet := NewQUICFleet(func(_ context.Context, index int, noteArrival func()) agentResult {
		if index == 0 {
			return agentResult{err: fmt.Errorf("enroll: context deadline exceeded")}
		}
		noteArrival()
		return agentResult{}
	})

	require.NoError(t, fleet.HoldConnected(0, 2))
	fleet.Stop()

	outcomes := fleet.Outcomes()
	assert.Equal(t, int64(1), outcomes.Failed)
	assert.Zero(t, outcomes.StoodDown)
}

// A machine that had already arrived and is then stood down is neither: it
// turned up, and the run ending its life is what the run is for.
func TestAnArrivedMachineStoodDownIsCountedAsArrived(t *testing.T) {
	fleet := NewQUICFleet(func(ctx context.Context, _ int, noteArrival func()) agentResult {
		noteArrival()
		<-ctx.Done()
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

// The results block counts the same way the fleet does, because the trend is
// built by reading it: a run that stood machines down reported them as machines
// that failed to connect, and the extraction turned that into an error rate the
// system had nothing to do with.
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
