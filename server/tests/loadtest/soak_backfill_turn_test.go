package main

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestAGrantIsFoundPastOtherMessages(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgRequestDeviceLogs, LogLimit: 5})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: &replies, w: &bytes.Buffer{}},
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10})

	require.NoError(t, err)
	assert.Equal(t, 1, sent, "a message pushed before the grant does not end the wait for it")
}

// quietThenAnswers passes its read deadline on its first reads, as an unanswered request does.
type quietThenAnswers struct {
	quiet     int
	replies   *bytes.Buffer
	sent      bytes.Buffer
	deadlines []time.Time
}

func (q *quietThenAnswers) Read(p []byte) (int, error) {
	if q.quiet > 0 {
		q.quiet--
		return 0, readDeadlinePassed{}
	}
	return q.replies.Read(p)
}

func (q *quietThenAnswers) Write(p []byte) (int, error) { return q.sent.Write(p) }

func (q *quietThenAnswers) SetReadDeadline(t time.Time) error {
	q.deadlines = append(q.deadlines, t)
	return nil
}

func TestATurnRequestWithNoAnswerIsAskedAgainAndCounted(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	stream := &quietThenAnswers{quiet: 2, replies: &replies}
	tally := &catchUpTally{}

	_, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10, catchUp: tally})

	require.NoError(t, err)
	count := tally.count()
	assert.EqualValues(t, 2, count.TurnRepeats, "every repeat is counted")
	assert.Equal(t, &CatchUpEndings{CaughtUp: 1}, count.Endings, "the machine caught up on its third ask")
	var asked int
	for stream.sent.Len() > 0 {
		if readControl(t, codec, &stream.sent).Type == protocol.MsgRequestBackfillSlot {
			asked++
		}
	}
	assert.Equal(t, 3, asked, "the turn was asked for again each time its answer did not come")
}

func TestATurnRequestWaitsAMinuteAndAFewSecondsForItsAnswer(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	stream := &quietThenAnswers{replies: &replies}

	_, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10})

	require.NoError(t, err)
	require.Len(t, stream.deadlines, 2, "the wait is set before the answer and cleared after it")
	wait := time.Until(stream.deadlines[0])
	assert.Greater(t, wait, turnAnswerWait-time.Second)
	assert.LessOrEqual(t, wait, turnAnswerWait+turnAnswerJitter)
	assert.True(t, stream.deadlines[1].IsZero(), "a batch's answer is read with no deadline")
}

func TestARunThatEndsWhileATurnIsUnansweredIsTheMachinesEnding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stream := &quietThenAnswers{quiet: 1, replies: &bytes.Buffer{}}
	tally := &catchUpTally{}

	_, err := drainBackfill(ctx, &protocol.Codec{}, stream,
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10, catchUp: tally})

	require.NoError(t, err)
	assert.Equal(t, &CatchUpEndings{RunEnded: 1}, tally.count().Endings)
	assert.Zero(t, tally.count().TurnRepeats, "a run that has ended asks nothing again")
}

func TestAConnectionThatIdlesOutWhileATurnIsAwaitedAsksNothingAgain(t *testing.T) {
	codec := &protocol.Codec{}
	stream := &idleThenGone{}
	tally := &catchUpTally{}

	_, err := drainBackfill(context.Background(), codec, stream,
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10, catchUp: tally})

	require.Error(t, err)
	assert.Zero(t, tally.count().TurnRepeats, "a connection gone quiet is not the turn's own wait running out")
	assert.Equal(t, &CatchUpEndings{NeverGranted: 1}, tally.count().Endings)
	var asked int
	for stream.sent.Len() > 0 {
		if readControl(t, codec, &stream.sent).Type == protocol.MsgRequestBackfillSlot {
			asked++
		}
	}
	assert.Equal(t, 1, asked, "the turn is asked for once")
}

// idleThenGone fails every read as a connection quic-go closed for silence does.
type idleThenGone struct {
	sent bytes.Buffer
}

func (s *idleThenGone) Read([]byte) (int, error) { return 0, &quic.IdleTimeoutError{} }

func (s *idleThenGone) Write(p []byte) (int, error) { return s.sent.Write(p) }

func (s *idleThenGone) SetReadDeadline(time.Time) error { return nil }

// readDeadlinePassed fails a read as a quic-go stream past its read deadline does.
type readDeadlinePassed struct{}

func (readDeadlinePassed) Error() string   { return "deadline exceeded" }
func (readDeadlinePassed) Timeout() bool   { return true }
func (readDeadlinePassed) Temporary() bool { return true }
func (readDeadlinePassed) Unwrap() error   { return os.ErrDeadlineExceeded }
