package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func arrived(at time.Time) agentResult {
	return agentResult{connectDur: time.Millisecond, arrivedAt: at}
}

func severed(at time.Time) agentResult {
	return agentResult{connectDur: time.Millisecond, arrivedAt: at, err: ErrHeldPeerGone}
}

func unreachable() agentResult {
	return agentResult{err: errors.New("dial: no route while the link is dark")}
}

func TestAMachineNotAskedToPersistLeavesWhenItsConnectionBreaks(t *testing.T) {
	calls := 0
	res := persistThrough(context.Background(), loadOptions{}, func(context.Context) agentResult {
		calls++
		return severed(time.Now())
	})

	assert.Equal(t, 1, calls, "a machine that was not asked to persist dials once")
	assert.ErrorIs(t, res.err, ErrHeldPeerGone, "and the severance is what it reports")
	assert.Zero(t, res.redials)
	assert.Zero(t, res.reconnected)
}

func TestAPersistentMachineComesBackAfterASeverance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	calls := 0
	res := persistThrough(ctx, loadOptions{reconnect: true}, func(context.Context) agentResult {
		calls++
		if calls == 1 {
			return severed(time.Now())
		}
		return arrived(time.Now())
	})

	assert.Equal(t, 2, calls)
	assert.NoError(t, res.err, "a machine that came back and held is not a failure")
	assert.Equal(t, 1, res.redials)
	assert.Equal(t, 1, res.reconnected, "the run's verdict says the herd was severed once")
}

func TestAPersistentMachineReportsASeveranceItNeverRecoveredFrom(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	first := true
	res := persistThrough(ctx, loadOptions{reconnect: true}, func(context.Context) agentResult {
		if first {
			first = false
			return severed(time.Now())
		}
		return unreachable()
	})

	assert.Error(t, res.err, "the machine was away when the run ended, and says so")
	assert.Zero(t, res.reconnected, "it never came back")
	assert.Positive(t, res.redials, "it did keep trying")
	assert.Error(t, ctx.Err(), "the run wound down while the machine was away")
}

func TestThePersistentMachineKeepsItsFirstArrival(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	first := time.Now()
	calls := 0
	res := persistThrough(ctx, loadOptions{reconnect: true}, func(context.Context) agentResult {
		calls++
		if calls == 1 {
			return severed(first)
		}
		return arrived(first.Add(5 * time.Minute))
	})

	assert.Equal(t, first, res.arrivedAt, "the arrival is the first one, not the return")
}

// deferThenGrant answers the slot request with n deferrals, then a grant, then acks every batch.
func deferThenGrant(t *testing.T, codec *protocol.Codec, deferrals, batches int) *pipeStream {
	t.Helper()
	var replies bytes.Buffer
	for i := 0; i < deferrals; i++ {
		writeControl(t, codec, &replies, &protocol.ControlMessage{
			Type: protocol.MsgDeferBackfill, RetryAfter: 0,
		})
	}
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	for i := 0; i < batches; i++ {
		writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	}
	return &pipeStream{r: &replies, w: &bytes.Buffer{}}
}

func TestADeferredMachineAsksAgainWhenItIsPersistent(t *testing.T) {
	codec := &protocol.Codec{}
	stream := deferThenGrant(t, codec, 2, 3)

	sent, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10, retryDeferred: true})

	require.NoError(t, err)
	assert.Equal(t, 3, sent, "the machine waited its turn and then caught up")
}

func TestADeferredMachineShedsLoadWhenItIsNot(t *testing.T) {
	codec := &protocol.Codec{}
	stream := deferThenGrant(t, codec, 1, 3)

	sent, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10})

	require.NoError(t, err)
	assert.Zero(t, sent, "one deferral ends the drain when the machine is not persistent")
}

func TestAPersistentMachineStopsAskingEventually(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	for i := 0; i < maxDeferrals+5; i++ {
		writeControl(t, codec, &replies, &protocol.ControlMessage{
			Type: protocol.MsgDeferBackfill, RetryAfter: 0,
		})
	}
	stream := &pipeStream{r: &replies, w: &bytes.Buffer{}}

	sent, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10, retryDeferred: true})

	require.NoError(t, err)
	assert.Zero(t, sent)
}

func TestADeferredMachineStopsAskingWhenTheRunWindsDown(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	for i := 0; i < 3; i++ {
		writeControl(t, codec, &replies, &protocol.ControlMessage{
			Type: protocol.MsgDeferBackfill, RetryAfter: 30,
		})
	}
	stream := &pipeStream{r: &replies, w: &bytes.Buffer{}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	sent, err := drainBackfill(ctx, codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10, retryDeferred: true})

	require.NoError(t, err)
	assert.Zero(t, sent)
	assert.Less(t, time.Since(started), 5*time.Second,
		"a cancelled run does not sit out the server's retry time")
}

func TestMachineNamesCarryTheRunsOwnPrefix(t *testing.T) {
	agents := planAgents(4, 2, "netdrill-fleet")

	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.hostname
	}
	assert.Equal(t, []string{
		"netdrill-fleet-t0-a0", "netdrill-fleet-t1-a1",
		"netdrill-fleet-t0-a2", "netdrill-fleet-t1-a3",
	}, names)
}

func TestDefaultMachineNamesAreUnchanged(t *testing.T) {
	agents := planAgents(2, 1, defaultHostnamePrefix)

	assert.Equal(t, "soak-t0-a0", agents[0].hostname)
	assert.Equal(t, "soak-t0-a1", agents[1].hostname)
}

func TestErrHeldPeerGoneSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("hold open: %w", ErrHeldPeerGone)
	assert.True(t, errors.Is(wrapped, ErrHeldPeerGone))
}
