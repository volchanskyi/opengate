package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// sentFrames decodes every control frame a machine wrote.
func sentFrames(t *testing.T, out *bytes.Buffer) []*protocol.ControlMessage {
	t.Helper()
	codec := &protocol.Codec{}
	var frames []*protocol.ControlMessage
	for out.Len() > 0 {
		frameType, payload, err := codec.ReadFrame(out)
		require.NoError(t, err)
		if frameType != protocol.FrameControl {
			continue
		}
		msg, err := codec.DecodeControl(payload)
		require.NoError(t, err)
		frames = append(frames, msg)
	}
	return frames
}

func TestAnInventoryIsPaddedToTheSizeAsked(t *testing.T) {
	t.Parallel()

	payload := inventoryPayload(1 << 20)

	assert.LessOrEqual(t, len(payload), 1<<20, "the server's ceiling holds")
	assert.Greater(t, len(payload), 1<<20-4096, "and the report fills it")
	msg, err := (&protocol.Codec{}).DecodeControl(payload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgDiscoveryReport, msg.Type)
	assert.NotEmpty(t, msg.Packages)
}

func TestAHeldMachineSendsTheInventoryItIsCalledForOnce(t *testing.T) {
	t.Parallel()
	call := &inventoryCall{}
	call.call(64 << 10)
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	require.NoError(t, holdOpen(context.Background(), &protocol.Codec{}, stream,
		loadOptions{holdFor: 300 * time.Millisecond, inventory: call}))

	var reports []*protocol.ControlMessage
	for _, msg := range sentFrames(t, stream.out) {
		if msg.Type == protocol.MsgDiscoveryReport {
			reports = append(reports, msg)
		}
	}
	require.Len(t, reports, 1, "one call is one report from each machine")
	assert.NotZero(t, reports[0].TS, "the report is stamped, as the server's interval floor reads it")
	assert.Equal(t, int64(1), call.sentCount())
}

func TestAMachineHeldWithNoCallSendsNoInventory(t *testing.T) {
	t.Parallel()
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	require.NoError(t, holdOpen(context.Background(), &protocol.Codec{}, stream,
		loadOptions{holdFor: 100 * time.Millisecond, inventory: &inventoryCall{}}))

	for _, msg := range sentFrames(t, stream.out) {
		assert.NotEqual(t, protocol.MsgDiscoveryReport, msg.Type)
	}
}

func TestAPhaseCallsItsInventoryAndCountsWhatTheServerStored(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	fleet := &recordingFleet{clock: clock}
	var asked []int
	call := &inventoryCall{}
	readings := PhaseReadings{Inventory: InventoryBurst{
		Call: func(bytes int) {
			asked = append(asked, bytes)
			call.call(bytes)
			call.noteSent()
			call.noteSent()
		},
		Sent:   call.sentCount,
		Stored: func() (int64, bool) { return 40 + int64(clock.now.Sub(start)/time.Minute), true },
	}}
	phase := Phase{Name: "inventory", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 2,
		InventoryBytes: 1 << 20}

	result, err := runOnePhase(phase, 2, fleet, clock, readings)
	require.NoError(t, err)

	assert.Equal(t, []int{1 << 20}, asked)
	require.NotNil(t, result.InventorySent)
	require.NotNil(t, result.InventoryStored)
	assert.Equal(t, int64(2), *result.InventorySent)
	assert.Equal(t, int64(1), *result.InventoryStored, "the server's count over the phase alone")
}

// storingOneEvery is a server that stores one inventory every interval from start, over a base.
func storingOneEvery(clock *testClock, start time.Time, interval time.Duration, asked *[]time.Duration) InventoryBurst {
	call := &inventoryCall{}
	return InventoryBurst{
		Call: call.call,
		Sent: call.sentCount,
		Stored: func() (int64, bool) {
			*asked = append(*asked, clock.now.Sub(start))
			return 100 + int64(clock.now.Sub(start)/interval), true
		},
	}
}

func TestAnInventoryPhaseTimesTheStoredCountReachingItsMachines(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	var asked []time.Duration
	readings := PhaseReadings{Inventory: storingOneEvery(clock, start, 10*time.Second, &asked)}
	phase := Phase{Name: "inventory", Duration: Duration{Duration: 2 * time.Minute}, ConnectedAgents: 4,
		InventoryBytes: 1 << 20}

	result, err := runOnePhase(phase, 4, &recordingFleet{clock: clock}, clock, readings)
	require.NoError(t, err)

	require.NotNil(t, result.InventoryStoredAllSeconds)
	assert.Equal(t, 40.0, *result.InventoryStoredAllSeconds, "the first 5 s mark with all four stored")
	assert.Equal(t, []time.Duration{0, 5 * time.Second, 10 * time.Second, 15 * time.Second,
		20 * time.Second, 25 * time.Second, 30 * time.Second, 35 * time.Second, 40 * time.Second,
		2 * time.Minute}, asked, "the count is asked every 5 s until it is reached, then at the end")
}

func TestAnInventoryPhaseWhoseCountNeverReachesItsMachinesHasNoTime(t *testing.T) {
	t.Parallel()
	start := time.Unix(1_800_000_000, 0)
	clock := &testClock{now: start}
	var asked []time.Duration
	readings := PhaseReadings{Inventory: storingOneEvery(clock, start, time.Minute, &asked)}
	phase := Phase{Name: "inventory", Duration: Duration{Duration: 2 * time.Minute}, ConnectedAgents: 4,
		InventoryBytes: 1 << 20}

	result, err := runOnePhase(phase, 4, &recordingFleet{clock: clock}, clock, readings)
	require.NoError(t, err)

	assert.Nil(t, result.InventoryStoredAllSeconds)
	require.NotNil(t, result.InventoryStored)
	assert.Equal(t, int64(2), *result.InventoryStored)
}

func TestAPhaseWithNoInventoryReadsNone(t *testing.T) {
	t.Parallel()
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	fleet := &recordingFleet{clock: clock}
	called := false
	readings := PhaseReadings{Inventory: InventoryBurst{Call: func(int) { called = true }}}

	result, err := runOnePhase(Phase{Name: "steady", Duration: Duration{Duration: time.Minute}, ConnectedAgents: 2},
		2, fleet, clock, readings)
	require.NoError(t, err)

	assert.False(t, called)
	assert.Nil(t, result.InventorySent)
	assert.Nil(t, result.InventoryStored)
	assert.Nil(t, result.InventoryStoredAllSeconds)
}

func TestTheServersStoredInventoriesAreReadOffItsPage(t *testing.T) {
	t.Parallel()

	stored, ok := parseInventoriesStored("# TYPE opengate_inventory_reports_stored_total counter\n" +
		"opengate_inventory_reports_stored_total 4999\nopengate_agents_connected 5000\n")

	require.True(t, ok)
	assert.Equal(t, int64(4999), stored)
	_, ok = parseInventoriesStored("opengate_agents_connected 5000\n")
	assert.False(t, ok, "a page with no count is unread, never nought")
}
