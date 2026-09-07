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

// A load generator dials once and reports a severance, which is the right
// behaviour for measuring what a server carries. A machine standing in for a
// customer's workstation during a link drill is a different thing: the site
// goes dark, and the machines come back. These cover the second behaviour and
// the switch that selects it, so the first one cannot be lost by accident.

// arrived is a connection that registered and then held until the run ended.
func arrived(at time.Time) agentResult {
	return agentResult{connectDur: time.Millisecond, arrivedAt: at}
}

// severed is a connection that registered and then lost its peer.
func severed(at time.Time) agentResult {
	return agentResult{connectDur: time.Millisecond, arrivedAt: at, err: ErrHeldPeerGone}
}

// unreachable is a dial into a dark link: nothing arrived, so there is no
// arrival to record and the machine is still away.
func unreachable() agentResult {
	return agentResult{err: errors.New("dial: no route while the link is dark")}
}

// TestAMachineNotAskedToPersistLeavesWhenItsConnectionBreaks pins the load
// generator's behaviour. A run that is measuring what the server carries wants
// a severance reported, not repaired, so the switch being off must mean exactly
// one connection.
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

// TestAPersistentMachineComesBackAfterASeverance is the herd behaviour the
// thin-uplink scenario depends on: the link goes dark, the machine's connection
// dies, and the machine is there again when the link returns.
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

// TestAPersistentMachineReportsASeveranceItNeverRecoveredFrom keeps the switch
// from turning a lost machine into a silent one. A machine that was still away
// when the run wound down was severed, and the verdict has to say so — that is
// the whole signal the drill reads to know its herd was there.
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

// TestThePersistentMachineKeepsItsFirstArrival guards the fleet's arrival
// window. A machine that came back later arrived once, when it first
// registered; reporting the second arrival would report the outage as part of
// how long the fleet took to assemble.
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

// --- asking again when the server says wait ------------------------------------

// deferThenGrant answers the machine's slot request with n deferrals and then a
// grant, acking every batch that follows. It is the scheduler's own shape: a
// deferred machine is handed a retry time and asked to come back.
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

// TestADeferredMachineAsksAgainWhenItIsPersistent is the second half of the
// herd. The server admits four catch-ups per customer and defers the rest with
// a retry time it shortens as they wait; a machine that gives up on the first
// deferral never catches up at all, so sixteen of twenty would sit out the
// window the staleness figure is measured across.
func TestADeferredMachineAsksAgainWhenItIsPersistent(t *testing.T) {
	codec := &protocol.Codec{}
	stream := deferThenGrant(t, codec, 2, 3)

	sent, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10, retryDeferred: true})

	require.NoError(t, err)
	assert.Equal(t, 3, sent, "the machine waited its turn and then caught up")
}

// TestADeferredMachineShedsLoadWhenItIsNot pins the load generator's behaviour
// against the switch above. A run measuring what the server carries wants a
// deferral honoured once and the load shed, not a machine that keeps asking.
func TestADeferredMachineShedsLoadWhenItIsNot(t *testing.T) {
	codec := &protocol.Codec{}
	stream := deferThenGrant(t, codec, 1, 3)

	sent, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10})

	require.NoError(t, err)
	assert.Zero(t, sent, "one deferral ends the drain when the machine is not persistent")
}

// TestAPersistentMachineStopsAskingEventually bounds the retry. A machine that
// is deferred forever is a finding about the scheduler, not a reason to keep a
// connection asking for the life of the run.
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

// TestADeferredMachineStopsAskingWhenTheRunWindsDown keeps a machine waiting
// out its retry time from outliving the run that started it.
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

// --- the run's own names -------------------------------------------------------

// TestMachineNamesCarryTheRunsOwnPrefix is what lets a drill count its own
// herd. Sharing a name with the load run leaves the drill unable to tell its
// twenty from anyone else's, and leaves its own machines to be swept by a
// cleanup that belongs to a different workflow.
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

// TestDefaultMachineNamesAreUnchanged holds the load run's names still. Its
// cleanup selects on them, so a rename here would strand every machine it makes.
func TestDefaultMachineNamesAreUnchanged(t *testing.T) {
	agents := planAgents(2, 1, defaultHostnamePrefix)

	assert.Equal(t, "soak-t0-a0", agents[0].hostname)
	assert.Equal(t, "soak-t0-a1", agents[1].hostname)
}

// TestErrHeldPeerGoneSurvivesWrapping keeps the severance recognisable through
// the layers the hold wraps it in, because that is what the persistence policy
// dispatches on.
func TestErrHeldPeerGoneSurvivesWrapping(t *testing.T) {
	wrapped := fmt.Errorf("hold open: %w", ErrHeldPeerGone)
	assert.True(t, errors.Is(wrapped, ErrHeldPeerGone))
}
