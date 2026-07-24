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

// loadOptions carries the Edge-Sentinel soak toggles: emitting the default
// telemetry shape (health summary + host metric window + process report — the
// WS-4 ingest path), emitting extra host-metric windows, answering on-demand
// raw-log pulls (the broker round-trip), and driving a reconnect-storm backfill
// drain (the WS-15 scheduler + tiered import path).
type loadOptions struct {
	defaultTelemetry        bool
	telemetryCycles         int
	metricWindows           int
	answerLogPulls          bool
	backfillBatches         int
	backfillSamplesPerBatch int
}

// defaultMetricDimNames are the host metric dimensions the sampler emits by
// default (mirrors the agent's store_sink series set). Central VM keeps avg
// only; min/max/last + 1 s raw stay agent-local.
var defaultMetricDimNames = []string{
	"cpu.total", "mem.used_percent", "disk.used_percent", "net.rx_bytes", "net.tx_bytes",
}

// defaultFamilies are the per-family anomaly-rate buckets a health summary
// reports beside the node-level rate.
var defaultFamilies = []string{"cpu", "memory", "disk", "network"}

// maxSoakLogLines bounds a soak DeviceLogsResponse so the agent side never
// answers a raw pull with an unbounded payload.
const maxSoakLogLines = 300

// answerPullDeadline bounds how long an agent waits for a raw pull before giving
// up, so a bare load run (no admin driving pulls) never blocks on the read.
const answerPullDeadline = 2 * time.Second

// soakStream is the subset of a QUIC stream the soak traffic uses.
type soakStream interface {
	io.ReadWriter
	SetReadDeadline(t time.Time) error
}

// buildExtraMetricWindow builds an AgentMetricWindow over the host-metric dims
// with an empty org (the server assigns the authoritative org from the
// connection). It drives extra WS-4 avg-series ingest load under multi-tenant
// stress, on top of the default telemetry shape.
func buildExtraMetricWindow(ts int64) *protocol.ControlMessage {
	dims := make([]protocol.MetricDim, len(defaultMetricDimNames))
	for i, name := range defaultMetricDimNames {
		dims[i] = protocol.MetricDim{Name: name, Avg: float64(i)}
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

// readControlFrame reads and decodes the next control frame, skipping any
// non-control frame type.
func readControlFrame(codec *protocol.Codec, r io.Reader) (*protocol.ControlMessage, error) {
	frameType, payload, err := codec.ReadFrame(r)
	if err != nil {
		return nil, err
	}
	if frameType != protocol.FrameControl {
		return nil, fmt.Errorf("unexpected frame type %d", frameType)
	}
	return codec.DecodeControl(payload)
}

// runSoakTraffic drives the Edge-Sentinel soak load for one agent: it emits the
// default telemetry shape and extra host-metric windows (ingest), runs a
// reconnect-storm backfill drain, and optionally answers one on-demand raw-log
// pull (the agent side of the broker round-trip).
func runSoakTraffic(codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if err := emitDefaultTelemetry(codec, stream, opts); err != nil {
		return err
	}
	if _, err := drainBackfill(codec, stream, opts); err != nil {
		return err
	}
	if err := emitMetricWindows(codec, stream, opts.metricWindows); err != nil {
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

// emitMetricWindows writes n host-metric windows, driving the ingest path.
func emitMetricWindows(codec *protocol.Codec, w io.Writer, n int) error {
	for i := 0; i < n; i++ {
		payload, err := codec.EncodeControl(buildExtraMetricWindow(time.Now().Unix()))
		if err != nil {
			return fmt.Errorf("encode metric window: %w", err)
		}
		if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
			return fmt.Errorf("write metric window: %w", err)
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
