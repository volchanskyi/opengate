package agentapi

import (
	"context"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// logsResult carries a bounded raw-log response (or an agent-side error) from
// the read loop back to the synchronous broker waiter.
type logsResult struct {
	entries []device.LogEntry
	total   int
	err     error
}

// RequestLogsSync brokers an on-demand raw-log pull: it sends a RequestDeviceLogs
// control message and blocks until the agent's response is delivered to the
// in-flight waiter or ctx expires. The bounded lines flow straight through to
// the caller — nothing is persisted centrally. Only one pull runs per connection
// at a time (responses carry no correlation id), so a concurrent caller gets
// ErrLogsBusy.
func (a *AgentConn) RequestLogsSync(ctx context.Context, filter device.LogFilter) ([]device.LogEntry, int, error) {
	if err := a.requireCapability(protocol.CapDeviceLogs); err != nil {
		return nil, 0, err
	}

	ch := make(chan logsResult, 1)
	a.logMu.Lock()
	if a.logWaiter != nil {
		a.logMu.Unlock()
		return nil, 0, ErrLogsBusy
	}
	a.logWaiter = ch
	a.logMu.Unlock()
	defer func() {
		a.logMu.Lock()
		a.logWaiter = nil
		a.logMu.Unlock()
	}()

	if err := a.SendRequestDeviceLogs(ctx, filter); err != nil {
		return nil, 0, err
	}

	select {
	case res := <-ch:
		return res.entries, res.total, res.err
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
}

// deliverLogs hands a response to the in-flight waiter, returning whether one
// was waiting. A late or unsolicited response with no waiter is dropped so the
// read loop never blocks.
func (a *AgentConn) deliverLogs(res logsResult) bool {
	a.logMu.Lock()
	ch := a.logWaiter
	a.logMu.Unlock()
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

// handleDeviceLogsResponse streams the agent's bounded raw-log response back to
// the waiting broker. Raw lines are transient: they are never written to any
// central store.
func (a *AgentConn) handleDeviceLogsResponse(_ context.Context, msg *protocol.ControlMessage) error {
	entries := make([]device.LogEntry, len(msg.LogEntries))
	for i, le := range msg.LogEntries {
		entries[i] = device.LogEntry{
			DeviceID:  a.DeviceID,
			Timestamp: le.Timestamp,
			Level:     le.Level,
			Target:    le.Target,
			Message:   le.Message,
		}
	}
	total := int(msg.TotalCount)
	if total < len(entries) {
		total = len(entries)
	}
	if !a.deliverLogs(logsResult{entries: entries, total: total}) {
		a.logger.Debug("dropping unsolicited device logs response", "device_id", a.DeviceID, "count", len(entries))
	}
	return nil
}

// handleDeviceLogsError routes an agent-side failure to the waiting broker.
func (a *AgentConn) handleDeviceLogsError(msg *protocol.ControlMessage) error {
	err := fmt.Errorf("agent device logs error: %s", msg.AckError)
	if !a.deliverLogs(logsResult{err: err}) {
		a.logger.Warn("device logs error from agent", "device_id", a.DeviceID, "error", msg.AckError)
	}
	return nil
}
