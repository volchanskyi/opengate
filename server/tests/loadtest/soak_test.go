package main

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// pipeStream adapts a read buffer and a write buffer into a soakStream, with a
// no-op read deadline, for exercising the bidirectional backfill drain offline.
type pipeStream struct {
	r io.Reader
	w io.Writer
}

func (p *pipeStream) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *pipeStream) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeStream) SetReadDeadline(time.Time) error {
	return nil
}

// writeControl frames one control message into buf, as the peer would send it.
func writeControl(t *testing.T, codec *protocol.Codec, buf *bytes.Buffer, msg *protocol.ControlMessage) {
	t.Helper()
	payload, err := codec.EncodeControl(msg)
	require.NoError(t, err)
	require.NoError(t, codec.WriteFrame(buf, protocol.FrameControl, payload))
}

// readControl decodes the next framed control message from buf.
func readControl(t *testing.T, codec *protocol.Codec, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	frameType, payload, err := codec.ReadFrame(buf)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

// TestDefaultTelemetryFrames builds the full default telemetry shape one agent
// emits each cycle: a health summary, a host metric window, and a minimal
// process report — the shape the WS-15b soak drives through the WS-4 ingest
// path. None of them asserts an org (the server assigns it from the connection).
func TestDefaultTelemetryFrames(t *testing.T) {
	frames := defaultTelemetryFrames(1_700_000_000)
	require.Len(t, frames, 3)

	types := []protocol.ControlMessageType{frames[0].Type, frames[1].Type, frames[2].Type}
	assert.Equal(t, []protocol.ControlMessageType{
		protocol.MsgAgentHealthSummary,
		protocol.MsgAgentMetricWindow,
		protocol.MsgProcessReport,
	}, types)

	for _, f := range frames {
		assert.Empty(t, f.OrgID, "agent must not assert an org; the server assigns it")
	}

	// The metric window carries the default host dims.
	window := frames[1]
	require.Len(t, window.Dims, len(defaultMetricDimNames))
	for i, d := range window.Dims {
		assert.Equal(t, defaultMetricDimNames[i], d.Name, "default window is host metrics")
	}
	// The process report is bounded and rank-ordered.
	require.NotEmpty(t, frames[2].TopN)
	assert.EqualValues(t, 1, frames[2].TopN[0].Rank)
}

// TestBuildTenantAgents deterministically partitions N tenants × M agents so a
// soak run is reproducible: every agent has a stable org index and a
// tenant-tagged hostname, and the org indices cover exactly [0, tenants).
func TestBuildTenantAgents(t *testing.T) {
	const tenants, perTenant = 5, 100
	agents := buildTenantAgents(tenants, perTenant)
	require.Len(t, agents, tenants*perTenant)

	seenOrg := map[int]int{}
	seenHost := map[string]struct{}{}
	for _, a := range agents {
		require.GreaterOrEqual(t, a.orgIndex, 0)
		require.Less(t, a.orgIndex, tenants)
		seenOrg[a.orgIndex]++
		_, dup := seenHost[a.hostname]
		require.False(t, dup, "hostnames must be unique: %q", a.hostname)
		seenHost[a.hostname] = struct{}{}
	}
	// Every tenant is represented, evenly.
	require.Len(t, seenOrg, tenants)
	for org, count := range seenOrg {
		assert.Equal(t, perTenant, count, "tenant %d agent count", org)
	}

	// The partition is deterministic: a second call is identical.
	assert.Equal(t, agents, buildTenantAgents(tenants, perTenant))
}

// TestBuildBackfillBatch builds a tiered reconnect-backfill batch with the
// original historical timestamps preserved, matching the agent replay engine's
// recent-first, one-acked-batch-at-a-time contract.
func TestBuildBackfillBatch(t *testing.T) {
	const n = 50
	start := int64(1_700_000_000)
	samples := buildBackfillSamples(n, start)
	require.Len(t, samples, n)

	batch := buildBackfillBatch(protocol.BackfillTierRaw10s, samples, samples[n-1].TS)
	assert.Equal(t, protocol.MsgMetricBackfillBatch, batch.Type)
	assert.Equal(t, protocol.BackfillTierRaw10s, batch.Tier)
	assert.Equal(t, samples[n-1].TS, batch.Cursor)
	require.Len(t, batch.BackfillSamples, n)
	// Timestamps are strictly increasing historical seconds; every sample names a
	// default host dim (central VM keeps avg only, so the batch carries the dim).
	for i, s := range batch.BackfillSamples {
		assert.NotEmpty(t, s.Name)
		if i > 0 {
			assert.Greater(t, s.TS, batch.BackfillSamples[i-1].TS)
		}
	}
}

// TestBackfillStormRoundTrip drives the agent side of the reconnect storm over
// an in-memory stream primed with a grant then an ack per batch, proving the
// harness sends a slot request, drains acked-one-at-a-time under the grant, and
// stops at the batch budget.
func TestBackfillStormRoundTrip(t *testing.T) {
	codec := &protocol.Codec{}

	// Prime the server side: one GrantBackfill, then one ack per batch.
	const batches = 3
	var serverToAgent bytes.Buffer
	grant := &protocol.ControlMessage{Type: protocol.MsgGrantBackfill, Rate: 2000, Deadline: 1_700_000_060}
	writeControl(t, codec, &serverToAgent, grant)
	for i := 0; i < batches; i++ {
		writeControl(t, codec, &serverToAgent, &protocol.ControlMessage{
			Type: protocol.MsgMetricBackfillAck, Tier: protocol.BackfillTierRaw10s, Cursor: int64(i),
		})
	}

	var agentToServer bytes.Buffer
	stream := &pipeStream{r: &serverToAgent, w: &agentToServer}
	opts := loadOptions{backfillBatches: batches, backfillSamplesPerBatch: 10}

	sent, err := drainBackfill(codec, stream, opts)
	require.NoError(t, err)
	assert.Equal(t, batches, sent, "all batches drain under a live grant")

	// The agent wrote a slot request followed by one batch per ack.
	slotReq := readControl(t, codec, &agentToServer)
	assert.Equal(t, protocol.MsgRequestBackfillSlot, slotReq.Type)
	assert.Positive(t, slotReq.PendingSamples)
	for i := 0; i < batches; i++ {
		b := readControl(t, codec, &agentToServer)
		assert.Equal(t, protocol.MsgMetricBackfillBatch, b.Type)
		assert.NotEmpty(t, b.BackfillSamples)
	}
}

// TestBackfillStormDeferIsNotAnError verifies a DeferBackfill reply ends the
// drain cleanly with zero batches sent — the scheduler shed load, which is a
// valid soak outcome, not a failure.
func TestBackfillStormDeferIsNotAnError(t *testing.T) {
	codec := &protocol.Codec{}
	var serverToAgent bytes.Buffer
	writeControl(t, codec, &serverToAgent, &protocol.ControlMessage{
		Type: protocol.MsgDeferBackfill, RetryAfter: 5,
	})
	var agentToServer bytes.Buffer
	stream := &pipeStream{r: &serverToAgent, w: &agentToServer}

	sent, err := drainBackfill(codec, stream, loadOptions{backfillBatches: 3, backfillSamplesPerBatch: 10})
	require.NoError(t, err)
	assert.Zero(t, sent, "a deferred storm drains nothing")
}

// TestSafeUint64 pins the non-negative narrowing used for the backlog hint: a
// positive count passes through, and a zero or negative count clamps to 0 so
// the int→uint64 conversion can never wrap into a huge PendingSamples value.
func TestSafeUint64(t *testing.T) {
	assert.Equal(t, uint64(42), safeUint64(42))
	assert.Equal(t, uint64(0), safeUint64(0))
	assert.Equal(t, uint64(0), safeUint64(-1))
}
