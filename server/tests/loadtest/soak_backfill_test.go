package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestACatchUpAnswerIsFoundPastOtherMessages(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgRequestDeviceLogs, LogLimit: 5})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: &replies, w: &bytes.Buffer{}},
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10, catchUp: tally})

	require.NoError(t, err)
	assert.Equal(t, 1, sent, "a message that is not the answer is passed over")
	assert.Equal(t, &CatchUpCount{Stored: 1, Complete: 1}, tally.count())
}

func TestADeferredBatchIsSentAgainAfterANewGrant(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgDeferBackfill, RetryAfter: 0})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	var written bytes.Buffer
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: &replies, w: &written},
		loadOptions{backfillBatches: 2, backfillSamplesPerBatch: 10, retryDeferred: true, catchUp: tally})

	require.NoError(t, err)
	assert.Equal(t, 2, sent)
	assert.Equal(t, &CatchUpCount{Stored: 2, Deferred: 1, Complete: 1}, tally.count(), "the machine caught up once")

	var batches []*protocol.ControlMessage
	for written.Len() > 0 {
		if msg := readControl(t, codec, &written); msg.Type == protocol.MsgMetricBackfillBatch {
			batches = append(batches, msg)
		}
	}
	require.Len(t, batches, 3, "the deferred batch goes out again before the next one")
	assert.Equal(t, batches[0].Cursor, batches[1].Cursor)
	assert.NotEqual(t, batches[1].Cursor, batches[2].Cursor)
}

func TestABatchWithNoAnswerIsCountedAndEndsTheDrain(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: &replies, w: &bytes.Buffer{}},
		loadOptions{backfillBatches: 2, backfillSamplesPerBatch: 10, catchUp: tally})

	require.Error(t, err, "the stream ended before the answer")
	assert.Zero(t, sent)
	assert.Equal(t, &CatchUpCount{Unanswered: 1}, tally.count())
}

func TestARunThatSentNoCatchUpReportsNone(t *testing.T) {
	assert.Nil(t, (&catchUpTally{}).count(), "nothing sent differs from nothing stored")
	var none *catchUpTally
	assert.Nil(t, none.count())
}

func TestTheRunReportSaysHowCatchUpWasAnswered(t *testing.T) {
	var out bytes.Buffer
	writeCatchUp(&out, &CatchUpCount{Stored: 40, Deferred: 3, Unanswered: 1})
	assert.Contains(t, out.String(), "Catch-up:    40 stored, 3 told to wait, 1 never answered")

	out.Reset()
	writeCatchUp(&out, nil)
	assert.Empty(t, out.String(), "a run that sent no catch-up reports none")
}

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
