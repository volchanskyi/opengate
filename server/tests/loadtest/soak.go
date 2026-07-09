package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// loadOptions carries the Edge-Sentinel soak toggles: emitting log-rate windows
// (the log-rate ingest path under multi-tenant load) and answering on-demand
// raw-log pulls (the agent side of the broker round-trip).
type loadOptions struct {
	logWindows     int
	answerLogPulls bool
}

// maxSoakLogLines bounds a soak DeviceLogsResponse so the agent side never
// answers a raw pull with an unbounded payload.
const maxSoakLogLines = 300

// answerPullDeadline bounds how long an agent waits for a raw pull before giving
// up, so a bare load run (no admin driving pulls) never blocks on the read.
const answerPullDeadline = 2 * time.Second

// logRateFields are the nine log-rate feature slots the agent reports, in WS-9
// slot order (five severities, three top-unit ranks, total volume).
var logRateFields = []string{
	"error", "warn", "info", "debug", "trace",
	"unit_rank1", "unit_rank2", "unit_rank3", "volume",
}

// soakStream is the subset of a QUIC stream the soak traffic uses.
type soakStream interface {
	io.ReadWriter
	SetReadDeadline(t time.Time) error
}

// buildLogRateWindow builds an AgentMetricWindow carrying only log-rate dims for
// the self source, with an empty org (the server assigns the authoritative org
// from the connection). It drives the WS-4 ingest path under multi-tenant load.
func buildLogRateWindow(ts int64) *protocol.ControlMessage {
	dims := make([]protocol.MetricDim, len(logRateFields))
	for i, field := range logRateFields {
		dims[i] = protocol.MetricDim{Name: "log.rate.self." + field, Avg: float64(i)}
	}
	return &protocol.ControlMessage{Type: protocol.MsgAgentMetricWindow, TS: ts, Dims: dims}
}

// buildDeviceLogsResponse builds a bounded DeviceLogsResponse for answering a
// raw pull during a soak. The requested count is clamped to maxSoakLogLines.
func buildDeviceLogsResponse(requested int) *protocol.ControlMessage {
	if requested <= 0 || requested > maxSoakLogLines {
		requested = maxSoakLogLines
	}
	entries := make([]protocol.LogEntry, requested)
	for i := range entries {
		entries[i] = protocol.LogEntry{
			Timestamp: "2026-01-01T00:00:00Z",
			Level:     "INFO",
			Target:    "loadtest",
			Message:   "soak log line",
		}
	}
	hasMore := false
	return &protocol.ControlMessage{
		Type:       protocol.MsgDeviceLogsResponse,
		LogEntries: entries,
		TotalCount: safeUint32(len(entries)),
		HasMore:    &hasMore,
	}
}

// safeUint32 narrows a non-negative int to uint32, clamping out-of-range values
// so the conversion cannot overflow (gosec G115).
func safeUint32(v int) uint32 {
	if v <= 0 {
		return 0
	}
	if uint64(v) > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(v)
}

// runSoakTraffic drives the Edge-Sentinel soak load for one agent: it emits the
// configured number of log-rate windows (log-rate ingest) and optionally answers
// one on-demand raw-log pull (the agent side of the broker round-trip).
func runSoakTraffic(codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if err := emitLogWindows(codec, stream, opts.logWindows); err != nil {
		return err
	}
	if !opts.answerLogPulls {
		return nil
	}
	if err := stream.SetReadDeadline(time.Now().Add(answerPullDeadline)); err != nil {
		return fmt.Errorf("set read deadline: %w", err)
	}
	// A missing pull within the deadline is expected in a bare run, so a read
	// timeout is not an error; only a mid-frame failure is.
	if _, err := answerLogPull(codec, stream, stream); err != nil && !isTimeout(err) {
		return fmt.Errorf("answer log pull: %w", err)
	}
	return nil
}

// emitLogWindows writes n log-rate metric windows, driving the ingest path.
func emitLogWindows(codec *protocol.Codec, w io.Writer, n int) error {
	for i := 0; i < n; i++ {
		payload, err := codec.EncodeControl(buildLogRateWindow(time.Now().Unix()))
		if err != nil {
			return fmt.Errorf("encode log window: %w", err)
		}
		if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
			return fmt.Errorf("write log window: %w", err)
		}
	}
	return nil
}

// answerLogPull reads one control frame; if it is a RequestDeviceLogs it writes
// a bounded DeviceLogsResponse and reports that it handled a pull. Any other
// frame is reported unhandled without a reply so the caller can dispatch it.
func answerLogPull(codec *protocol.Codec, r io.Reader, w io.Writer) (bool, error) {
	frameType, payload, err := codec.ReadFrame(r)
	if err != nil {
		return false, err
	}
	if frameType != protocol.FrameControl {
		return false, nil
	}
	msg, err := codec.DecodeControl(payload)
	if err != nil {
		return false, err
	}
	if msg.Type != protocol.MsgRequestDeviceLogs {
		return false, nil
	}
	respPayload, err := codec.EncodeControl(buildDeviceLogsResponse(int(msg.LogLimit)))
	if err != nil {
		return false, err
	}
	if err := codec.WriteFrame(w, protocol.FrameControl, respPayload); err != nil {
		return false, err
	}
	return true, nil
}

// isTimeout reports whether err is an i/o timeout, which the soak treats as "no
// pull arrived" rather than a failure.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
