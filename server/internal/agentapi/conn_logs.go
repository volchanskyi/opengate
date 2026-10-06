package agentapi

import (
	"context"
	"fmt"

	"github.com/volchanskyi/opengate/server/internal/device"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// logsResult carries a raw-log response or agent-side error from the read loop to the waiter.
type logsResult struct {
	entries []device.LogEntry
	total   int
	units   []string
	err     error
}

// RequestLogsSync requests raw log lines and blocks until the agent responds or ctx expires.
// Responses carry no correlation id, so a concurrent caller gets ErrLogsBusy.
func (a *AgentConn) RequestLogsSync(ctx context.Context, filter device.LogFilter) ([]device.LogEntry, int, []string, error) {
	if err := a.requireCapability(protocol.CapDeviceLogs); err != nil {
		return nil, 0, nil, err
	}

	ch := make(chan logsResult, 1)
	a.logMu.Lock()
	if a.logWaiter != nil {
		a.logMu.Unlock()
		return nil, 0, nil, ErrLogsBusy
	}
	a.logWaiter = ch
	a.logMu.Unlock()
	defer func() {
		a.logMu.Lock()
		a.logWaiter = nil
		a.logMu.Unlock()
	}()

	if err := a.SendRequestDeviceLogs(ctx, filter); err != nil {
		return nil, 0, nil, err
	}

	select {
	case res := <-ch:
		return res.entries, res.total, res.units, res.err
	case <-ctx.Done():
		return nil, 0, nil, ctx.Err()
	}
}

// deliverLogs hands a response to the in-flight waiter and reports whether one was waiting; an
// unsolicited response is dropped so the read loop never blocks.
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

// handleDeviceLogsResponse passes the raw-log response to the waiter; raw lines are never written
// to a central store.
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
	if !a.deliverLogs(logsResult{entries: entries, total: total, units: msg.AvailableUnits}) {
		a.logger.Debug("dropping unsolicited device logs response", "device_id", a.DeviceID, "count", len(entries))
	}
	return nil
}

func (a *AgentConn) handleDeviceLogsError(msg *protocol.ControlMessage) error {
	err := fmt.Errorf("agent device logs error: %s", msg.AckError)
	if !a.deliverLogs(logsResult{err: err}) {
		a.logger.Warn("device logs error from agent", "device_id", a.DeviceID, "error", msg.AckError)
	}
	return nil
}
