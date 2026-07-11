package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// historyResult carries a bounded deep-history response (or an agent-side error)
// from the read loop back to the synchronous broker waiter.
type historyResult struct {
	points    []protocol.HistoryPoint
	truncated bool
	err       error
}

// RequestLocalHistorySync brokers an on-demand deep-history pull: it sends a
// RequestLocalHistory control message for one dimension over a bounded window and
// blocks until the agent's response reaches the in-flight waiter or ctx expires.
// The full-resolution points flow straight through to the caller. Only one pull
// runs per connection at a time (responses carry no correlation id), so a
// concurrent caller gets ErrHistoryBusy. Cross-tenant access is denied upstream
// by the device-ownership check in the API handler; this broker only ever talks
// to the one connected agent it belongs to.
func (a *AgentConn) RequestLocalHistorySync(ctx context.Context, dim string, fromTS, toTS int64, maxPoints uint32) ([]protocol.HistoryPoint, bool, error) {
	if err := a.requireCapability(protocol.CapBackfill); err != nil {
		return nil, false, err
	}

	ch := make(chan historyResult, 1)
	a.historyMu.Lock()
	if a.historyWaiter != nil {
		a.historyMu.Unlock()
		return nil, false, ErrHistoryBusy
	}
	a.historyWaiter = ch
	a.historyMu.Unlock()
	defer func() {
		a.historyMu.Lock()
		a.historyWaiter = nil
		a.historyMu.Unlock()
	}()

	if err := a.SendRequestLocalHistory(ctx, dim, fromTS, toTS, maxPoints); err != nil {
		return nil, false, err
	}

	select {
	case res := <-ch:
		return res.points, res.truncated, res.err
	case <-ctx.Done():
		return nil, false, ctx.Err()
	}
}

// deliverHistory hands a response to the in-flight waiter, returning whether one
// was waiting. A late or unsolicited response with no waiter is dropped so the
// read loop never blocks.
func (a *AgentConn) deliverHistory(res historyResult) bool {
	a.historyMu.Lock()
	ch := a.historyWaiter
	a.historyMu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- res:
		return true
	default:
		return false
	}
}

// handleLocalHistoryResponse routes the agent's bounded deep-history response
// back to the waiting broker.
func (a *AgentConn) handleLocalHistoryResponse(msg *protocol.ControlMessage) error {
	truncated := msg.Truncated != nil && *msg.Truncated
	if !a.deliverHistory(historyResult{points: msg.HistoryPoints, truncated: truncated}) {
		a.logger.Debug("dropping unsolicited local history response", "device_id", a.DeviceID, "count", len(msg.HistoryPoints))
	}
	return nil
}
