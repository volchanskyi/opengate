package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"sync/atomic"
	"time"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// loadOptions carries the soak toggles: default telemetry, extra host-metric windows,
// raw-log pull answers and the reconnect-storm backfill drain.
type loadOptions struct {
	defaultTelemetry        bool
	telemetryCycles         int
	metricWindows           int
	answerLogPulls          bool
	backfillBatches         int
	backfillSamplesPerBatch int

	// holdFor keeps each agent connected after its traffic, so sessions can open against it.
	holdFor time.Duration

	// relaySessions answers a SessionRequest by joining the machine side of the relay and
	// echoing, so the browser side times its own frame coming back.
	relaySessions bool

	// sessionsJoined counts the machine sides this run answered across every agent, one per
	// completed session. Nil counts nothing.
	sessionsJoined *atomic.Int64

	// reconnect keeps a machine in the run after its connection breaks. Off by default so a
	// severance is reported.
	reconnect bool

	// retryDeferred makes a machine ask again when the server defers its catch-up slot.
	// Off by default so a load run sheds the load and measures the deferral path.
	retryDeferred bool
}

// defaultMetricDimNames is every host metric dimension a machine writes, in window order.
// The full stored set is written because load cost is per-series.
var defaultMetricDimNames = []string{
	"cpu.total", "cpu.total.max",
	"mem.used_percent", "mem.used_percent.max",
	"disk.used_percent",
	"net.rx_bps", "net.rx_bps.max",
	"net.tx_bps", "net.tx_bps.max",
	"disk.mounts_critical",
	"stall.cpu.some", "stall.mem.some", "stall.mem.full", "stall.io.some", "stall.io.full",
	"disk.await_ms", "disk.await_ms.max", "disk.queue_depth",
}

// defaultFamilies are the per-family anomaly-rate buckets a health summary reports beside the
// node-level rate, named as the server accounts for them.
var defaultFamilies = []string{"cpu", "mem", "disk", "net", "proc"}

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

// buildExtraMetricWindow builds an AgentMetricWindow over the host-metric dims with an empty
// tenant, which the server assigns from the connection.
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

// runSoakTraffic drives the soak load for one agent: telemetry, backfill drain, metric windows,
// an optional raw-log pull answer, then holds the connection open.
func runSoakTraffic(ctx context.Context, codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if err := emitDefaultTelemetry(codec, stream, opts); err != nil {
		return err
	}
	if _, err := drainBackfill(ctx, codec, stream, opts); err != nil {
		return err
	}
	if err := emitMetricWindows(codec, stream, opts.metricWindows); err != nil {
		return err
	}
	if opts.answerLogPulls {
		if err := stream.SetReadDeadline(time.Now().Add(answerPullDeadline)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		// A bare run sees no pull within the deadline, so only a mid-frame failure is an error.
		if _, err := answerLogPull(codec, stream, stream); err != nil && !isTimeout(err) {
			return fmt.Errorf("answer log pull: %w", err)
		}
	}
	return holdOpen(ctx, codec, stream, opts)
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

// answerLogPull reads one control frame and answers a RequestDeviceLogs with a bounded
// DeviceLogsResponse; any other frame is reported unhandled without a reply.
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

// isTimeout reports whether err is an i/o timeout, which the soak reads as no pull arriving.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
