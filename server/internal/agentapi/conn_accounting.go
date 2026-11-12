package agentapi

import (
	"context"
	"time"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

const (
	// maxTelemetryBacklog bounds how far behind the server clock a live agent-stamped timestamp
	// may sit; backfill carries its own wider bound.
	maxTelemetryBacklog = 7 * 24 * time.Hour
	// maxTelemetrySkew bounds how far ahead of the server clock an agent-stamped timestamp may sit.
	maxTelemetrySkew = 5 * time.Minute
	// clampFuture and clampPast are the direction labels of
	// opengate_edge_telemetry_clock_clamped_total.
	clampFuture = "future"
	clampPast   = "past"
)

// telemetryFloor names one ten-second floor: a message type and, for a health summary, its kind.
type telemetryFloor struct {
	msgType protocol.ControlMessageType
	kind    string
}

// The kinds a message type's floor is kept per: none for most, two for a health summary.
const (
	floorPerType   = ""
	summaryAnomaly = "anomaly"
	summaryBreach  = "breach"
)

func (a *AgentConn) acceptTelemetry(msgType protocol.ControlMessageType, kind string, ts int64, payloadLen int) bool {
	floor := telemetryFloor{msgType: msgType, kind: kind}
	if payloadLen > maxTelemetryPayloadBytes {
		a.dropTelemetry("payload_too_large", "type", msgType, "bytes", payloadLen)
		return false
	}
	if ts <= 0 {
		return a.acceptedTelemetry(msgType)
	}
	if a.telemetryLast == nil {
		a.telemetryLast = make(map[telemetryFloor]int64)
	}
	if last, ok := a.telemetryLast[floor]; ok && ts-last < minTelemetryIntervalSeconds {
		a.dropTelemetry("interval_floor", "type", msgType, "kind", floor.kind, "ts", ts, "last_ts", last)
		return false
	}
	a.telemetryLast[floor] = ts
	return a.acceptedTelemetry(msgType)
}

// acceptedTelemetry counts one accepted message against the ingest counter and returns true.
func (a *AgentConn) acceptedTelemetry(msgType protocol.ControlMessageType) bool {
	if a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryIngest(string(msgType))
	}
	return true
}

// persistTelemetry runs fn on a bounded slot goroutine so a slow store never stalls the read
// loop, and reports whether fn was started; a failure drops one per message the write carries.
func (a *AgentConn) persistTelemetry(ctx context.Context, msgs int, fn func(context.Context, dbtx.Tenant) error) bool {
	tenant, ok := dbtx.TenantFromContext(ctx)
	if !ok {
		a.dropTelemetryN(msgs, "tenant_missing")
		return false
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
		return true
	default:
		a.dropTelemetryN(msgs, "persist_slots_full")
		return false
	}
}

// dropTelemetry records one discarded telemetry message under reason.
func (a *AgentConn) dropTelemetry(reason string, args ...any) {
	a.dropTelemetryN(1, reason, args...)
}

// dropTelemetryN records n discarded telemetry messages under one reason, counting a batch per
// message. An n of 0 logs nothing and counts nothing.
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

// telemetryTimestamp returns the time a sample is written at, pulling a stamp outside the
// accepted window to the nearer bound and counting the correction by direction.
func (a *AgentConn) telemetryTimestamp(ts int64) time.Time {
	stamped, direction := clampTelemetryTimestamp(ts, time.Now().UTC())
	a.observeClockClamp(direction)
	return stamped
}

// observeClockClamp counts a clock correction; an empty direction counts nothing.
func (a *AgentConn) observeClockClamp(direction string) {
	if direction != "" && a.metrics != nil {
		a.metrics.ObserveEdgeTelemetryClockClamp(direction)
	}
}

// clampTelemetryTimestamp maps a stamp into [now-maxTelemetryBacklog, now+maxTelemetrySkew] and
// reports the bound it hit; a missing stamp takes now. The mapping is monotone.
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
