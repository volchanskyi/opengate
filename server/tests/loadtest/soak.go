package main

import (
	"context"
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

	// holdFor keeps each agent connected after its traffic. A machine that
	// connects and leaves exercises the accept path and nothing that happens
	// afterwards, and a generator on the other side needs machines that are
	// still there to open sessions against.
	holdFor time.Duration

	// relaySessions answers a SessionRequest by joining the machine side of the
	// relay and echoing. The browser side then times its own frame coming back,
	// which is what makes a relay latency figure a measurement of the relay.
	relaySessions bool
}

// defaultMetricDimNames is every host metric dimension a machine writes, in the
// order a window carries them: each gauge's average, then its window maximum
// where a within-minute spike is the signal, then the stall vitals and the
// disk-performance vitals.
//
// It is the whole stored vocabulary rather than a sample of it, because the
// cost a load run is measuring is per-series: a window carrying part of the set
// writes fewer series, occupies less of the per-device budget and finishes
// sooner than the one production actually receives, so the run would report a
// server absorbing a load nobody sends.
//
// telemetry_shape_test.go holds this equal to the cross-language golden the
// agent and the server already agree through.
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

// defaultFamilies are the per-family anomaly-rate buckets a health summary
// reports beside the node-level rate. These are the names the server accounts
// for, so a summary carrying them lands in the series a dashboard reads.
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

// buildExtraMetricWindow builds an AgentMetricWindow over the host-metric dims
// with an empty tenant (the server assigns the authoritative tenant from the
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
// reconnect-storm backfill drain, optionally answers one on-demand raw-log pull
// (the agent side of the broker round-trip), and then holds the connection open
// for as long as the run asked, answering whatever the server sends.
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
	if opts.answerLogPulls {
		if err := stream.SetReadDeadline(time.Now().Add(answerPullDeadline)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		// A missing pull within the deadline is expected in a bare run, so a
		// read timeout is not an error; only a mid-frame failure is.
		if _, err := answerLogPull(codec, stream, stream); err != nil && !isTimeout(err) {
			return fmt.Errorf("answer log pull: %w", err)
		}
	}
	return holdOpen(codec, stream, opts)
}

// holdOpen keeps this machine in the run for opts.holdFor, answering what the
// server sends. A fleet's steady-state cost is connections that stay up, not
// connections that open; a harness that disconnects immediately never applies
// it.
//
// A read that times out is the ordinary case — a quiet server has nothing to
// say — so the loop simply reads again until the hold is over.
func holdOpen(codec *protocol.Codec, stream soakStream, opts loadOptions) error {
	if opts.holdFor <= 0 {
		return nil
	}

	deadline := time.Now().Add(opts.holdFor)
	for time.Now().Before(deadline) {
		wait := min(holdReadSlice, time.Until(deadline))
		if err := stream.SetReadDeadline(time.Now().Add(wait)); err != nil {
			return fmt.Errorf("set read deadline: %w", err)
		}
		msg, err := readControlFrame(codec, stream)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			return fmt.Errorf("hold open: %w", err)
		}
		if err := answerHeldFrame(codec, stream, msg, opts); err != nil {
			return err
		}
	}
	return nil
}

// holdReadSlice bounds one read while a machine is held open, so the hold ends
// close to when it was asked to rather than at the next frame the server
// happens to send.
const holdReadSlice = 2 * time.Second

// answerHeldFrame replies to what the server sends a held-open machine. Only
// the frames a load run needs to keep moving are answered; anything else is
// read and discarded, which is what an agent that does not support a capability
// does.
func answerHeldFrame(codec *protocol.Codec, w io.Writer, msg *protocol.ControlMessage, opts loadOptions) error {
	switch msg.Type {
	case protocol.MsgRequestDeviceLogs:
		payload, err := codec.EncodeControl(buildDeviceLogsResponse(int(msg.LogLimit)))
		if err != nil {
			return fmt.Errorf("encode device logs response: %w", err)
		}
		if err := codec.WriteFrame(w, protocol.FrameControl, payload); err != nil {
			return fmt.Errorf("write device logs response: %w", err)
		}
	case protocol.MsgSessionRequest:
		if !opts.relaySessions {
			return nil
		}
		return joinRequestedSession(msg)
	}
	return nil
}

// joinRequestedSession opens the machine side of the session the server just
// handed this connection and echoes on it until the operator's side goes away.
//
// It runs on its own goroutine because the relay is a separate connection: the
// control stream must stay readable, or the machine stops answering everything
// else for as long as somebody has a session open.
func joinRequestedSession(msg *protocol.ControlMessage) error {
	req, err := RelayRequestFrom(msg)
	if err != nil {
		return fmt.Errorf("session request: %w", err)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), relaySessionLifetime)
		defer cancel()

		joined, err := JoinRelay(ctx, req)
		if err != nil {
			return
		}
		defer func() { _ = joined.Close() }()
		_ = joined.Echo(ctx)
	}()
	return nil
}

// relaySessionLifetime bounds one simulated session, so a run cannot leave a
// goroutine echoing into a pipe nobody is reading.
const relaySessionLifetime = 5 * time.Minute

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
