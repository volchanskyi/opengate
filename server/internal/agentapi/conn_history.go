package agentapi

import (
	"context"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// historyResult carries a history response or agent-side error from the read loop to the waiter.
type historyResult struct {
	points    []protocol.HistoryPoint
	truncated bool
	err       error
}

// RequestLocalHistorySync blocks until the agent returns a dimension's history or ctx expires; a
// concurrent caller gets ErrHistoryBusy because responses carry no correlation id.
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

// deliverHistory hands a response to the in-flight waiter and reports whether one was waiting; an
// unsolicited response is dropped so the read loop never blocks.
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

func (a *AgentConn) handleLocalHistoryResponse(msg *protocol.ControlMessage) error {
	truncated := msg.Truncated != nil && *msg.Truncated
	if !a.deliverHistory(historyResult{points: msg.HistoryPoints, truncated: truncated}) {
		a.logger.Debug("dropping unsolicited local history response", "device_id", a.DeviceID, "count", len(msg.HistoryPoints))
	}
	return nil
}
