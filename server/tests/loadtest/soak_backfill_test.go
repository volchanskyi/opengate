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
	from := replies(t, codec, grantReply, &protocol.ControlMessage{Type: protocol.MsgRequestDeviceLogs, LogLimit: 5},
		storedReply)
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: from, w: &bytes.Buffer{}},
		loadOptions{backfillBatches: 1, backfillSamplesPerBatch: 10, catchUp: tally})

	require.NoError(t, err)
	assert.Equal(t, 1, sent, "a message that is not the answer is passed over")
	assert.Equal(t, &CatchUpCount{Stored: 1, Complete: 1, Endings: &CatchUpEndings{CaughtUp: 1}}, tally.count())
}

func TestADeferredBatchIsSentAgainAfterANewGrant(t *testing.T) {
	codec := &protocol.Codec{}
	from := replies(t, codec, grantReply, waitReply, grantReply, storedReply, storedReply)
	var written bytes.Buffer
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: from, w: &written},
		loadOptions{backfillBatches: 2, backfillSamplesPerBatch: 10, retryDeferred: true, catchUp: tally})

	require.NoError(t, err)
	assert.Equal(t, 2, sent)
	assert.Equal(t, &CatchUpCount{Stored: 2, Deferred: 1, Complete: 1, Endings: &CatchUpEndings{CaughtUp: 1}},
		tally.count(), "the machine caught up once")

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
	tally := &catchUpTally{}

	sent, err := drainBackfill(context.Background(), codec, &pipeStream{r: replies(t, codec, grantReply), w: &bytes.Buffer{}},
		loadOptions{backfillBatches: 2, backfillSamplesPerBatch: 10, catchUp: tally})

	require.Error(t, err, "the stream ended before the answer")
	assert.Zero(t, sent)
	assert.Equal(t, &CatchUpCount{Unanswered: 1, Endings: &CatchUpEndings{ConnectionEnded: 1}}, tally.count())
}

var (
	grantReply  = &protocol.ControlMessage{Type: protocol.MsgGrantBackfill}
	storedReply = &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck}
	waitReply   = &protocol.ControlMessage{Type: protocol.MsgDeferBackfill}
)

// replies is the server's side of one drain, each message in turn.
func replies(t *testing.T, codec *protocol.Codec, msgs ...*protocol.ControlMessage) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	for _, m := range msgs {
		writeControl(t, codec, &b, m)
	}
	return &b
}

// drainCounted drains a machine's planned batches of ten samples against from, one batch unless
// opts names more, and returns the tally it added to.
func drainCounted(ctx context.Context, t *testing.T, codec *protocol.Codec, from *bytes.Buffer,
	opts loadOptions,
) (*catchUpTally, error) {
	t.Helper()
	if opts.catchUp == nil {
		opts.catchUp = &catchUpTally{}
	}
	opts.backfillBatches = max(opts.backfillBatches, 1)
	opts.backfillSamplesPerBatch = 10
	_, err := drainBackfill(ctx, codec, &pipeStream{r: from, w: &bytes.Buffer{}}, opts)
	return opts.catchUp, err
}

// waitedOut is a scheduler telling a machine to wait once past the limit; after a first grant,
// each wait is followed by a new grant.
func waitedOut(t *testing.T, codec *protocol.Codec, first ...*protocol.ControlMessage) *bytes.Buffer {
	t.Helper()
	msgs := first
	for range maxDeferrals + 1 {
		msgs = append(msgs, waitReply)
		if len(first) > 0 {
			msgs = append(msgs, grantReply)
		}
	}
	return replies(t, codec, msgs...)
}

func TestEachMachineSaysHowItsCatchUpEnded(t *testing.T) {
	toldToWait := &CatchUpEndings{ToldToWait: 1, ToldToWaitTimes: map[int]int64{maxDeferrals + 1: 1}}
	cases := []struct {
		name    string
		from    func(*testing.T, *protocol.Codec) *bytes.Buffer
		opts    loadOptions
		runOver bool
		broken  bool
		want    *CatchUpEndings
	}{
		{name: "a machine that caught up says so",
			from: func(t *testing.T, c *protocol.Codec) *bytes.Buffer { return replies(t, c, grantReply, storedReply) },
			want: &CatchUpEndings{CaughtUp: 1}},
		{name: "a turn that never came is the machine's ending",
			from: func(*testing.T, *protocol.Codec) *bytes.Buffer { return &bytes.Buffer{} },
			opts: loadOptions{backfillBatches: 2}, broken: true, want: &CatchUpEndings{NeverGranted: 1}},
		{name: "a machine told to wait too often says how many times",
			from: func(t *testing.T, c *protocol.Codec) *bytes.Buffer { return waitedOut(t, c, grantReply) },
			opts: loadOptions{retryDeferred: true}, want: toldToWait},
		{name: "a turn refused past the limit is a wait like a refused batch",
			from: func(t *testing.T, c *protocol.Codec) *bytes.Buffer { return waitedOut(t, c) },
			opts: loadOptions{retryDeferred: true}, want: toldToWait},
		{name: "a connection that breaks after the grant is the machine's ending",
			from: func(t *testing.T, c *protocol.Codec) *bytes.Buffer { return replies(t, c, grantReply) },
			opts: loadOptions{backfillBatches: 2}, broken: true, want: &CatchUpEndings{ConnectionEnded: 1}},
		{name: "a run that ends while a machine waits is the machine's ending",
			from: func(t *testing.T, c *protocol.Codec) *bytes.Buffer {
				return replies(t, c, grantReply, &protocol.ControlMessage{Type: protocol.MsgDeferBackfill, RetryAfter: 30})
			},
			opts: loadOptions{retryDeferred: true}, runOver: true, want: &CatchUpEndings{RunEnded: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.runOver {
				cancel()
			}
			codec := &protocol.Codec{}

			tally, err := drainCounted(ctx, t, codec, tc.from(t, codec), tc.opts)

			if tc.broken {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, tally.count(), "a machine that sent nothing still says why")
			assert.Equal(t, tc.want, tally.count().Endings)
		})
	}
}

func TestASecondCatchUpOnOneConnectionIsNoSecondEnding(t *testing.T) {
	codec := &protocol.Codec{}
	tally, err := drainCounted(context.Background(), t, codec, replies(t, codec, grantReply, storedReply),
		loadOptions{resending: true})

	require.NoError(t, err)
	assert.Nil(t, tally.count().Endings, "a machine sending again already said how its catch-up ended")
}

func TestAMachineThatCatchesUpOnTwoConnectionsIsOneMachineCaughtUp(t *testing.T) {
	codec := &protocol.Codec{}
	tally := &catchUpTally{}
	for range 2 {
		_, err := drainCounted(context.Background(), t, codec, replies(t, codec, grantReply, storedReply),
			loadOptions{machine: "contoso-db-01", catchUp: tally})
		require.NoError(t, err)
	}

	count := tally.count()
	assert.EqualValues(t, 1, count.Complete, "a machine that reconnected and caught up again is one machine")
	assert.Equal(t, &CatchUpEndings{CaughtUp: 1}, count.Endings)
}
