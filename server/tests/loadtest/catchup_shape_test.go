package main

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestAnHourGapIsEveryVitalReadingOfEveryClosedMinute(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 30, 17, 0, time.UTC)

	batches := gapBatches(time.Hour, now, 1000)

	var all []protocol.BackfillSample
	for _, batch := range batches {
		assert.LessOrEqual(t, len(batch), 1000, "a batch holds at most what a machine sends at once")
		all = append(all, batch...)
	}
	require.Len(t, all, 60*len(defaultMetricDimNames), "one reading per vital per minute of the hour")
	assert.Len(t, batches, 2)

	closed := now.Truncate(time.Minute).Unix()
	seen := map[[2]any]bool{}
	for _, s := range all {
		assert.Less(t, s.TS, closed, "the minute still running is never sent")
		assert.GreaterOrEqual(t, s.TS, closed-3600)
		assert.Zero(t, s.TS%60, "each reading sits on its minute")
		key := [2]any{s.Name, s.TS}
		assert.False(t, seen[key], "no reading is sent twice")
		seen[key] = true
	}
}

func TestEachBatchEndsAtTheNewestMinuteItCarries(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 30, 0, 0, time.UTC)
	for _, batch := range gapBatches(10*time.Minute, now, 50) {
		newest := batch[0].TS
		for _, s := range batch {
			newest = max(newest, s.TS)
		}
		assert.Equal(t, newest, batch[len(batch)-1].TS, "the cursor a batch is confirmed at is its newest reading")
	}
}

func TestNoGapPlansNoBatch(t *testing.T) {
	assert.Empty(t, gapBatches(0, time.Now(), 1000))
	assert.Empty(t, gapBatches(30*time.Second, time.Now(), 1000), "less than a closed minute is nothing to catch up")
}

func TestACountedDrainPlansTheBatchesItWasAskedFor(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	batches := plannedBatches(loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10}, now)
	require.Len(t, batches, 3)
	for _, batch := range batches {
		assert.Len(t, batch, 10)
	}

	gap := plannedBatches(loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10, backfillGap: time.Hour}, now)
	assert.Len(t, gap, 2, "a gap sizes the drain from the hour it lost")
}

func TestAHeldMachineSendsAWindowEachPeriod(t *testing.T) {
	codec := &protocol.Codec{}
	written := &frameRecorder{}
	stream := &quietStream{w: written}
	opts := loadOptions{holdFor: time.Hour, liveEvery: 40 * time.Millisecond}

	deadline := time.Now().Add(220 * time.Millisecond)
	require.NoError(t, proveConnection(codec, stream, opts, func() time.Duration { return time.Until(deadline) }))

	windows := written.count(t, codec, protocol.MsgAgentMetricWindow)
	assert.GreaterOrEqual(t, windows, 4, "one window each period while held")
	assert.LessOrEqual(t, windows, 6)
}

// quietStream is a stream from a server that sends nothing: a read waits out its deadline, as a
// QUIC stream does, and reports a timeout.
type quietStream struct {
	w        io.Writer
	deadline time.Time
}

func (q *quietStream) Read([]byte) (int, error) {
	time.Sleep(time.Until(q.deadline))
	return 0, timeoutError{}
}

func (q *quietStream) Write(p []byte) (int, error) { return q.w.Write(p) }

func (q *quietStream) SetReadDeadline(t time.Time) error {
	q.deadline = t
	return nil
}

// frameRecorder keeps every frame a machine writes.
type frameRecorder struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (f *frameRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.buf.Write(p)
}

// count decodes every recorded frame and counts the control messages of one type.
func (f *frameRecorder) count(t *testing.T, codec *protocol.Codec, msgType protocol.ControlMessageType) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for f.buf.Len() > 0 {
		if readControl(t, codec, &f.buf).Type == msgType {
			n++
		}
	}
	return n
}

func TestAMachineCatchingUpAGapAdvertisesCatchUp(t *testing.T) {
	assert.Contains(t, agentCapabilities(loadOptions{backfillGap: time.Hour}), protocol.CapBackfill,
		"a server ignores catch-up from a machine that never said it sends any")
	assert.Contains(t, agentCapabilities(loadOptions{backfillBatches: 2}), protocol.CapBackfill)
	assert.NotContains(t, agentCapabilities(loadOptions{}), protocol.CapBackfill)
}

func TestALiveWindowIsStampedWhenItIsSent(t *testing.T) {
	codec := &protocol.Codec{}
	var written bytes.Buffer
	before := time.Now().Unix()
	require.NoError(t, sendLiveWindow(codec, &written, loadOptions{}))
	after := time.Now().Unix()

	window := readControl(t, codec, &written)
	assert.Equal(t, protocol.MsgAgentMetricWindow, window.Type)
	assert.GreaterOrEqual(t, window.TS, before, "a minute's window follows the one the machine sent on arrival")
	assert.LessOrEqual(t, window.TS, after)
}
