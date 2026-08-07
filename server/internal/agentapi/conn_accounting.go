package agentapi

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// maxTelemetryBacklog bounds how far behind the server clock an agent-stamped
	// live-telemetry timestamp may sit. It covers the longest backlog a bounded
	// local queue can present after a reconnect and stays well inside central
	// retention, so a host whose clock is days behind cannot write history the
	// charts would read as real. Reconnect backfill carries its own, far wider
	// bound (backfillRetentionSecs) because replaying months of pre-rolled
	// history is exactly what that path is for.
	maxTelemetryBacklog = 7 * 24 * time.Hour
	// maxTelemetrySkew bounds how far ahead of the server clock an agent-stamped
	// timestamp may sit. It is wider than any drift NTP corrects on its own and
	// narrower than the vitals bucket window, so an ordinary drifting host passes
	// through untouched while a hard jump of hours is pulled back to now.
	maxTelemetrySkew = 5 * time.Minute
	// clampFuture and clampPast are the direction labels of
	// opengate_edge_telemetry_clock_clamped_total.
	clampFuture = "future"
	clampPast   = "past"
)

func (a *AgentConn) acceptTelemetry(msgType protocol.ControlMessageType, ts int64, payloadLen int) bool {
	if payloadLen > maxTelemetryPayloadBytes {
		a.dropTelemetry("payload_too_large", "type", msgType, "bytes", payloadLen)
		return false
	}
	if ts <= 0 {
		return a.acceptedTelemetry(msgType)
	}
	if a.telemetryLast == nil {
		a.telemetryLast = make(map[protocol.ControlMessageType]int64)
	}
	if last, ok := a.telemetryLast[msgType]; ok && ts-last < minTelemetryIntervalSeconds {
		a.dropTelemetry("interval_floor", "type", msgType, "ts", ts, "last_ts", last)
		return false
	}
	a.telemetryLast[msgType] = ts
	return a.acceptedTelemetry(msgType)
}

// acceptedTelemetry records one accepted telemetry message against the ingest
// counter and returns true, so callers can `return a.acceptedTelemetry(...)`.
func (a *AgentConn) acceptedTelemetry(msgType protocol.ControlMessageType) bool {
	if a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryIngest(string(msgType))
	}
	return true
}

// persistTelemetry runs fn on a bounded slot goroutine so a slow store never
// stalls the read loop. msgs is how many ingested messages this write carries,
// so a failure reports one drop per message and the ingest ledger stays balanced;
// a write that is a second copy of a message already accounted for elsewhere
// passes 0.
func (a *AgentConn) persistTelemetry(ctx context.Context, msgs int, fn func(context.Context, dbtx.Tenant) error) {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetryN(msgs, "tenant_missing")
		return
	}
	if a.telemetrySlots == nil {
		a.telemetrySlots = make(chan struct{}, telemetryConcurrentWrites)
	}
	select {
	case a.telemetrySlots <- struct{}{}:
		go func() {
			defer func() { <-a.telemetrySlots }()
			jobCtx, cancel := context.WithTimeout(ctx, telemetryPersistTimeout)
			defer cancel()
			if err := fn(jobCtx, tenant); err != nil {
				a.dropTelemetryN(msgs, "persist_failed", "error", err)
			}
		}()
	default:
		a.dropTelemetryN(msgs, "persist_slots_full")
	}
}

// dropTelemetry records one discarded telemetry message under reason.
func (a *AgentConn) dropTelemetry(reason string, args ...any) {
	a.dropTelemetryN(1, reason, args...)
}

// dropTelemetryN records n discarded telemetry messages under one reason, so a
// coalesced batch that never reaches its store is counted per message rather
// than per batch. n of 0 logs nothing and counts nothing.
func (a *AgentConn) dropTelemetryN(n int, reason string, args ...any) {
	if n <= 0 {
		return
	}
	a.telemetryDrops.Add(uint64(n))
	if a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryDrop(reason, n)
	}
	if a.logger != nil {
		a.logger.Debug("dropping edge sentinel telemetry",
			append([]any{"device_id", a.DeviceID, "reason", reason, "messages", n}, args...)...)
	}
}

// telemetryTimestamp turns an agent-stamped second into the timestamp a sample
// is written at, pulling a host clock outside the accepted window back to the
// nearer bound and counting the correction by direction. A clamped message is
// still persisted — only its timestamp changes.
func (a *AgentConn) telemetryTimestamp(ts int64) time.Time {
	stamped, direction := clampTelemetryTimestamp(ts, time.Now().UTC())
	a.observeClockClamp(direction)
	return stamped
}

// observeClockClamp records a clock correction, if there was one. An empty
// direction means the stamp was already inside the window and nothing is
// counted.
func (a *AgentConn) observeClockClamp(direction string) {
	if direction != "" && a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryClockClamp(direction)
	}
}

// clampTelemetryTimestamp maps an agent-stamped second into
// [now-maxTelemetryBacklog, now+maxTelemetrySkew] and reports which bound it hit
// (clampPast, clampFuture, or empty when it was already inside). A missing
// stamp takes the server clock. The mapping is monotone, so a batch clamped
// sample by sample keeps the order the agent sent it in.
func clampTelemetryTimestamp(ts int64, now time.Time) (time.Time, string) {
	if ts <= 0 {
		return now, ""
	}
	stamped := time.Unix(ts, 0).UTC()
	if floor := now.Add(-maxTelemetryBacklog); stamped.Before(floor) {
		return floor, clampPast
	}
	if ceil := now.Add(maxTelemetrySkew); stamped.After(ceil) {
		return ceil, clampFuture
	}
	return stamped, ""
}
