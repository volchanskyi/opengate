package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// TestRequestLocalHistorySync_DeliversResponse pins the deep-history broker: a
// synchronous pull blocks until the agent's LocalHistoryResponse is delivered to
// the in-flight waiter, and the bounded full-res points flow straight through.
func TestRequestLocalHistorySync_DeliversResponse(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}

	type result struct {
		points    []protocol.HistoryPoint
		truncated bool
		err       error
	}
	resCh := make(chan result, 1)
	go func() {
		points, truncated, err := ac.RequestLocalHistorySync(context.Background(), "cpu.total", 1000, 2000, 500)
		resCh <- result{points, truncated, err}
	}()

	require.Eventually(t, func() bool {
		return ac.deliverHistory(historyResult{
			points:    []protocol.HistoryPoint{{TS: 1000, Value: 10}, {TS: 1001, Value: 11}},
			truncated: true,
		})
	}, time.Second, 2*time.Millisecond)

	got := <-resCh
	require.NoError(t, got.err)
	assert.True(t, got.truncated)
	require.Len(t, got.points, 2)
	assert.Equal(t, int64(1001), got.points[1].TS)
	assert.InEpsilon(t, 11.0, got.points[1].Value, 0.0001)
}

// TestRequestLocalHistorySync_RequiresCapability refuses before writing anything
// when the agent never advertised the Backfill capability.
func TestRequestLocalHistorySync_RequiresCapability(t *testing.T) {
	ac, buf := newTestAgentConn(t, uuid.New(), nil)

	_, _, err := ac.RequestLocalHistorySync(context.Background(), "cpu.total", 1, 2, 10)
	assert.ErrorIs(t, err, ErrCapabilityNotAdvertised)
	assert.Zero(t, buf.Len())
}

// TestRequestLocalHistorySync_SingleFlight rejects a second concurrent pull for
// the same connection (responses carry no correlation id).
func TestRequestLocalHistorySync_SingleFlight(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}
	ac.historyWaiter = make(chan historyResult, 1) // simulate an in-flight pull

	_, _, err := ac.RequestLocalHistorySync(context.Background(), "cpu.total", 1, 2, 10)
	assert.ErrorIs(t, err, ErrHistoryBusy)
}

// TestRequestLocalHistorySync_Timeout returns the context error when the agent
// never responds, and clears the waiter so a later pull is not reported busy.
func TestRequestLocalHistorySync_Timeout(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := ac.RequestLocalHistorySync(ctx, "cpu.total", 1, 2, 10)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, ac.historyWaiter)
}

// TestHandleLocalHistoryResponse_DeliversToWaiter routes an agent response
// through the read-loop handler to the waiting broker. It retries the handler
// (whose delivery is mutex-guarded) until the waiter is registered rather than
// racing on the unexported waiter field.
func TestHandleLocalHistoryResponse_DeliversToWaiter(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	ac.Capabilities = []protocol.AgentCapability{protocol.CapBackfill}

	type result struct {
		points []protocol.HistoryPoint
		err    error
	}
	resCh := make(chan result, 1)
	go func() {
		points, _, err := ac.RequestLocalHistorySync(context.Background(), "cpu.total", 1, 100, 500)
		resCh <- result{points, err}
	}()

	truncated := false
	require.Eventually(t, func() bool {
		_ = ac.handleLocalHistoryResponse(&protocol.ControlMessage{
			Type:          protocol.MsgLocalHistoryResponse,
			Dim:           "cpu.total",
			HistoryPoints: []protocol.HistoryPoint{{TS: 42, Value: 3.5}},
			Truncated:     &truncated,
		})
		return len(resCh) > 0
	}, time.Second, 5*time.Millisecond)

	got := <-resCh
	require.NoError(t, got.err)
	require.Len(t, got.points, 1)
	assert.Equal(t, int64(42), got.points[0].TS)
}

// TestHandleLocalHistoryResponse_DropsUnsolicited never blocks the read loop when
// no pull is waiting.
func TestHandleLocalHistoryResponse_DropsUnsolicited(t *testing.T) {
	ac, _ := newTestAgentConn(t, uuid.New(), nil)
	assert.NoError(t, ac.handleLocalHistoryResponse(&protocol.ControlMessage{
		Type:          protocol.MsgLocalHistoryResponse,
		HistoryPoints: []protocol.HistoryPoint{{TS: 1, Value: 1}},
	}))
}
