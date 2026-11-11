package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// ingestPage is the server's own account of its live readings, as its metrics page prints it.
func ingestPage(received, stored, withinFive float64) string {
	return fmt.Sprintf(`# HELP opengate_ingest_readings_received_total Readings handed to the numeric writer, by class.
opengate_ingest_readings_received_total{class="live"} %g
opengate_ingest_readings_received_total{class="catch_up"} 99999
opengate_ingest_readings_total{class="live",outcome="stored"} %g
opengate_ingest_readings_total{class="live",outcome="queue_full"} 3
opengate_ingest_acceptance_seconds_bucket{class="live",le="4"} 1
opengate_ingest_acceptance_seconds_bucket{class="live",le="5"} %g
opengate_ingest_acceptance_seconds_bucket{class="catch_up",le="5"} 77777
`, received, stored, withinFive)
}

func TestTheLiveAcceptanceIsTheServersOwnCountWithDropsAsMisses(t *testing.T) {
	before, err := ParseLiveAcceptance(ingestPage(100, 90, 80))
	require.NoError(t, err)
	after, err := ParseLiveAcceptance(ingestPage(10_100, 10_040, 9_980))
	require.NoError(t, err)

	run := after.Since(before)

	assert.Equal(t, LiveAcceptance{Received: 10_000, Stored: 9_950, WithinFiveSeconds: 9_900}, run)
	assert.InDelta(t, 0.99, run.Share(), 1e-9, "a dropped reading counts against the promise")
	assert.Zero(t, LiveAcceptance{}.Share(), "a run that received nothing kept no promise")
}

func TestAPageWithoutTheWritersMetricIsRefused(t *testing.T) {
	_, err := ParseLiveAcceptance("opengate_http_requests_total 4\n")
	require.Error(t, err)
}

func TestAMachineSendsItsLiveWindowAtTheSecondItsNameGives(t *testing.T) {
	second := liveSendSecond("contoso-desk-0042")
	assert.Equal(t, second, liveSendSecond("contoso-desk-0042"), "every minute at the same second")
	minute := time.Unix(1_763_000_040, 0)
	assert.Equal(t, time.Duration(second)*time.Second, liveSendDelay(minute, second)%time.Minute)
	assert.Equal(t, time.Minute, liveSendDelay(minute.Add(time.Duration(second)*time.Second), second),
		"a machine at its second waits the whole next minute")

	perSecond := map[int]int{}
	for i := range 5_000 {
		perSecond[liveSendSecond(fmt.Sprintf("contoso-desk-%04d", i))]++
	}
	for s := range 60 {
		assert.InDeltaf(t, 83, perSecond[s], 45, "second %d carries about a sixtieth of the fleet", s)
	}
}

func TestAResendRequestDuringTheHoldCatchesUpFromItsTime(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgGrantBackfill})
	for range 4 {
		writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgMetricBackfillAck})
	}
	var written bytes.Buffer
	tally := &catchUpTally{}
	since := time.Now().Add(-5 * time.Minute)

	err := answerResendRequest(context.Background(), codec, &pipeStream{r: &replies, w: &written},
		loadOptions{backfillGap: time.Hour, catchUp: tally},
		&protocol.ControlMessage{Type: protocol.MsgDeferBackfill, SinceTS: since.Unix()})

	require.NoError(t, err)
	var oldest int64
	for written.Len() > 0 {
		msg := readControl(t, codec, &written)
		if msg.Type == protocol.MsgMetricBackfillBatch {
			for _, s := range msg.BackfillSamples {
				if oldest == 0 || s.TS < oldest {
					oldest = s.TS
				}
			}
		}
	}
	assert.GreaterOrEqual(t, oldest, since.Truncate(time.Minute).Unix()-60,
		"the machine sends again from the requested time, not its whole first gap")
	assert.Equal(t, int64(1), tally.count().ResendRequests)
}

func TestALiveConfirmationIsCounted(t *testing.T) {
	tally := &catchUpTally{}
	opts := loadOptions{catchUp: tally}
	require.NoError(t, answerHeldFrame(&protocol.Codec{}, &bytes.Buffer{},
		&protocol.ControlMessage{Type: protocol.MsgLiveWindowStored, TS: 1_763_000_040}, opts))
	writeControlTallyBatch(tally)
	assert.Equal(t, int64(1), tally.count().LiveConfirmed)
}

// writeControlTallyBatch records one stored batch, so the tally reports beside the confirmation.
func writeControlTallyBatch(tally *catchUpTally) { tally.record(batchStored) }

func TestAMachineCatchingUpReadsConfirmationsAndResendRequests(t *testing.T) {
	caps := agentCapabilities(loadOptions{backfillGap: time.Hour})
	assert.Contains(t, caps, protocol.CapLiveConfirmations)
	assert.Contains(t, caps, protocol.CapResendRequests)
	assert.NotContains(t, agentCapabilities(loadOptions{}), protocol.CapResendRequests)
}
