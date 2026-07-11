package agentapi

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// backfillConn builds an AgentConn wired to a scheduler over an in-memory
// buffer, advertising the Backfill capability unless capable is false.
func backfillConn(t *testing.T, s *BackfillScheduler, org uuid.UUID, capable bool) (*AgentConn, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:  uuid.New(),
		OrgID:     org,
		stream:    &buf,
		codec:     &protocol.Codec{},
		scheduler: s,
		logger:    testLogger(),
	}
	if capable {
		ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}
	}
	return ac, &buf
}

// readReply decodes the single control frame the handler wrote back to buf.
func readReply(t *testing.T, ac *AgentConn, buf *bytes.Buffer) *protocol.ControlMessage {
	t.Helper()
	frameType, payload, err := ac.codec.ReadFrame(buf)
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := ac.codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

func TestHandleRequestBackfillSlot_GrantsWhenAdmitted(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	org := uuid.New()
	ac, buf := backfillConn(t, s, org, true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type:           protocol.MsgRequestBackfillSlot,
		PendingSamples: 5000,
		OldestTS:       1_699_000_000,
	}))

	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgGrantBackfill, reply.Type)
	assert.GreaterOrEqual(t, reply.Rate, schedCfg().MinGrantRate)
	assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), reply.Deadline)
	// The slot is booked against the connection's org, never an agent-supplied one.
	assert.Equal(t, 1, s.ActiveCount())
}

func TestHandleRequestBackfillSlot_DefersWhenSaturated(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	// Saturate the global cap with unrelated agents.
	for range schedCfg().MaxConcurrent {
		require.True(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)
	}
	ac, buf := backfillConn(t, s, uuid.New(), true)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type: protocol.MsgRequestBackfillSlot,
	}))

	reply := readReply(t, ac, buf)
	assert.Equal(t, protocol.MsgDeferBackfill, reply.Type)
	assert.Positive(t, reply.RetryAfter)
}

func TestHandleRequestBackfillSlot_IgnoredWithoutCapability(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	ac, buf := backfillConn(t, s, uuid.New(), false)

	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{
		Type: protocol.MsgRequestBackfillSlot,
	}))
	assert.Zero(t, buf.Len(), "no reply is written for an agent without the Backfill capability")
	assert.Equal(t, 0, s.ActiveCount(), "no slot is booked")
}

func TestHandleRequestBackfillSlot_NoSchedulerIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	ac := &AgentConn{
		DeviceID:     uuid.New(),
		stream:       &buf,
		codec:        &protocol.Codec{},
		Capabilities: []protocol.AgentCapability{protocol.CapBackfill},
		logger:       testLogger(),
	}
	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}))
	assert.Zero(t, buf.Len())
}

func TestHandleRequestBackfillSlot_ScopesToConnectionOrg(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	connOrg := uuid.New()
	ac, buf := backfillConn(t, s, connOrg, true)

	// Fill the connection's org to its per-tenant cap using the SAME org id the
	// connection carries; the next request must defer — proving admission keys on
	// the connection org, not anything the agent could supply.
	for range schedCfg().PerTenantMax {
		require.True(t, s.RequestSlot(uuid.New(), connOrg, SlotRequest{}).Grant)
	}
	require.NoError(t, ac.handleRequestBackfillSlot(&protocol.ControlMessage{Type: protocol.MsgRequestBackfillSlot}))
	assert.Equal(t, protocol.MsgDeferBackfill, readReply(t, ac, buf).Type)
}
