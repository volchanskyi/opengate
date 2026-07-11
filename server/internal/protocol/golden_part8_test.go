package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// WS-15 offline-backfill wire contract. These forward goldens are Rust-encoded
// and verified here for byte-level struct fidelity. The reconnect-backfill
// scheduler (RequestBackfillSlot/GrantBackfill/DeferBackfill), the tiered
// replay (MetricBackfillBatch/MetricBackfillAck), and the on-demand deep-history
// pull (RequestLocalHistory/LocalHistoryResponse) are all additive and gated by
// the Backfill capability.

func TestGoldenControlRequestBackfillSlot(t *testing.T) {
	msg := decodeControlFrame(t, "control_request_backfill_slot.bin")
	assert.Equal(t, MsgRequestBackfillSlot, msg.Type)
	assert.Equal(t, uint64(123456), msg.PendingSamples)
	assert.Equal(t, int64(1700000000), msg.OldestTS)
}

func TestGoldenControlGrantBackfill(t *testing.T) {
	msg := decodeControlFrame(t, "control_grant_backfill.bin")
	assert.Equal(t, MsgGrantBackfill, msg.Type)
	assert.Equal(t, uint32(500), msg.Rate)
	assert.Equal(t, int64(1700003600), msg.Deadline)
}

func TestGoldenControlDeferBackfill(t *testing.T) {
	msg := decodeControlFrame(t, "control_defer_backfill.bin")
	assert.Equal(t, MsgDeferBackfill, msg.Type)
	assert.Equal(t, uint32(30), msg.RetryAfter)
}

func TestGoldenControlMetricBackfillBatch(t *testing.T) {
	msg := decodeControlFrame(t, "control_metric_backfill_batch.bin")
	assert.Equal(t, MsgMetricBackfillBatch, msg.Type)
	assert.Equal(t, BackfillTierRollup1m, msg.Tier)
	assert.Equal(t, int64(1700000060), msg.Cursor)
	require.Len(t, msg.BackfillSamples, 2)
	assert.Equal(t, "cpu.total", msg.BackfillSamples[0].Name)
	assert.Equal(t, int64(1700000000), msg.BackfillSamples[0].TS)
	assert.InEpsilon(t, 42.5, msg.BackfillSamples[0].Value, 0.0001)
	assert.Equal(t, "mem.rss", msg.BackfillSamples[1].Name)
	assert.Equal(t, int64(1700000060), msg.BackfillSamples[1].TS)
	assert.InEpsilon(t, 2048.0, msg.BackfillSamples[1].Value, 0.0001)
}

func TestGoldenControlMetricBackfillAck(t *testing.T) {
	msg := decodeControlFrame(t, "control_metric_backfill_ack.bin")
	assert.Equal(t, MsgMetricBackfillAck, msg.Type)
	assert.Equal(t, BackfillTierRollup1m, msg.Tier)
	assert.Equal(t, int64(1700000060), msg.Cursor)
}

func TestGoldenControlRequestLocalHistory(t *testing.T) {
	msg := decodeControlFrame(t, "control_request_local_history.bin")
	assert.Equal(t, MsgRequestLocalHistory, msg.Type)
	assert.Equal(t, "cpu.total", msg.Dim)
	assert.Equal(t, int64(1699990000), msg.FromTS)
	assert.Equal(t, int64(1700000000), msg.ToTS)
	assert.Equal(t, uint32(1000), msg.MaxPoints)
}

func TestGoldenControlLocalHistoryResponse(t *testing.T) {
	msg := decodeControlFrame(t, "control_local_history_response.bin")
	assert.Equal(t, MsgLocalHistoryResponse, msg.Type)
	assert.Equal(t, "cpu.total", msg.Dim)
	require.Len(t, msg.HistoryPoints, 2)
	assert.Equal(t, int64(1699990000), msg.HistoryPoints[0].TS)
	assert.InEpsilon(t, 10.0, msg.HistoryPoints[0].Value, 0.0001)
	assert.Equal(t, int64(1699990001), msg.HistoryPoints[1].TS)
	assert.InEpsilon(t, 11.0, msg.HistoryPoints[1].Value, 0.0001)
	require.NotNil(t, msg.Truncated)
	assert.True(t, *msg.Truncated)
}
