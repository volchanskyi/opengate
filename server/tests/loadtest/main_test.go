package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// buildExtraMetricWindow produces an AgentMetricWindow over the host-metric dims
// with an empty tenant (the server assigns the authoritative tenant from the
// connection), mirroring the agent's live host-metric emission.
func TestBuildExtraMetricWindow(t *testing.T) {
	msg := buildExtraMetricWindow(1_700_000_000)

	assert.Equal(t, protocol.MsgAgentMetricWindow, msg.Type)
	assert.EqualValues(t, 1_700_000_000, msg.TS)
	assert.Empty(t, msg.TenantID, "agent must not assert a tenant; the server assigns it")
	require.Len(t, msg.Dims, len(defaultMetricDimNames))
	for i, dim := range msg.Dims {
		assert.Equal(t, defaultMetricDimNames[i], dim.Name,
			"every dim must be a host-metric dim")
	}
}

// buildDeviceLogsResponse produces a bounded DeviceLogsResponse so an agent side
// of the soak can answer raw pulls without unbounded payloads.
func TestBuildDeviceLogsResponse(t *testing.T) {
	msg := buildDeviceLogsResponse(500)
	assert.Equal(t, protocol.MsgDeviceLogsResponse, msg.Type)
	assert.LessOrEqual(t, len(msg.LogEntries), maxSoakLogLines,
		"response line count must be bounded")
	assert.EqualValues(t, len(msg.LogEntries), msg.TotalCount)
}

// answerLogPull replies to a RequestDeviceLogs control frame with a bounded
// DeviceLogsResponse and reports that it handled a pull.
func TestAnswerLogPull_RepliesToRequest(t *testing.T) {
	codec := &protocol.Codec{}
	req := &protocol.ControlMessage{Type: protocol.MsgRequestDeviceLogs, LogLimit: 50}
	payload, err := codec.EncodeControl(req)
	require.NoError(t, err)

	var in bytes.Buffer
	require.NoError(t, codec.WriteFrame(&in, protocol.FrameControl, payload))

	var out bytes.Buffer
	handled, err := answerLogPull(codec, &in, &out)
	require.NoError(t, err)
	assert.True(t, handled, "a RequestDeviceLogs frame must be answered")

	// The reply is a decodable, bounded DeviceLogsResponse.
	frameType, respPayload, err := codec.ReadFrame(&out)
	require.NoError(t, err)
	assert.EqualValues(t, protocol.FrameControl, frameType)
	resp, err := codec.DecodeControl(respPayload)
	require.NoError(t, err)
	assert.Equal(t, protocol.MsgDeviceLogsResponse, resp.Type)
	assert.LessOrEqual(t, len(resp.LogEntries), maxSoakLogLines)
}

// A non-pull control frame is left for other handlers and reported as unhandled
// without writing a reply.
func TestAnswerLogPull_IgnoresOtherFrames(t *testing.T) {
	codec := &protocol.Codec{}
	other := &protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat, Timestamp: 1}
	payload, err := codec.EncodeControl(other)
	require.NoError(t, err)

	var in bytes.Buffer
	require.NoError(t, codec.WriteFrame(&in, protocol.FrameControl, payload))

	var out bytes.Buffer
	handled, err := answerLogPull(codec, &in, &out)
	require.NoError(t, err)
	assert.False(t, handled, "a non-pull frame is not a raw pull")
	assert.Zero(t, out.Len(), "no reply is written for a non-pull frame")
}

// What the process returns is the only thing the runner around it can read, so
// the three outcomes a run has must be three different codes.
//
// A run that connected nobody produced no failed agents — it produced no agents
// at all — so a code derived from the failure count alone reports it as a clean
// run. That is how a sweep came to read as partly working when none of it was.
func TestTheExitCodeSaysWhichOfTheThreeOutcomesHappened(t *testing.T) {
	t.Run("a run that measured nothing is not a clean run", func(t *testing.T) {
		verdict := Verdict{Result: ResultInvalid, Reasons: []string{"scenario \"quic-agents\" produced no rows"}}
		assert.Equal(t, exitMeasuredNothing, exitCode(verdict, 0),
			"no failures and no measurement is the shape that used to pass")
	})

	t.Run("a fleet that half arrived is a measurement", func(t *testing.T) {
		assert.Equal(t, exitAgentFailures, exitCode(Verdict{Result: ResultValid}, 12),
			"a fleet that half connects is exactly what the trend exists to record")
	})

	t.Run("a run that breached a gate is a measurement too", func(t *testing.T) {
		assert.Equal(t, exitAgentFailures, exitCode(Verdict{Result: ResultFailed}, 0),
			"a gate breach is a finding about the system, not a missing run")
	})

	t.Run("a whole fleet that arrived and held is clean", func(t *testing.T) {
		assert.Equal(t, 0, exitCode(Verdict{Result: ResultValid}, 0))
	})

	t.Run("measuring nothing outranks the failure count", func(t *testing.T) {
		assert.Equal(t, exitMeasuredNothing, exitCode(Verdict{Result: ResultInvalid}, 500),
			"a run that never measured the system cannot be reported as one that did")
	})
}

// The QUIC half's aggregate rate is machines divided by a duration, and which
// duration decides whether the number is a rate at all.
//
// A run holds its fleet connected so the relay generator beside it has machines
// to open sessions against. The hold is most of the run's wall clock — eight
// minutes against a fleet that arrives in under a second — so dividing by the
// run's own length reports the hold, not the arrival. A hundred machines that
// all arrived read as 0.196/s against a floor of 50, every night, however
// healthy the server was.
//
// The window is therefore measured to the last arrival: the moment the slowest
// machine finished registering, which is when the fleet is up.
func TestTheArrivalWindowEndsWhenTheFleetIsUpNotWhenTheRunIs(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	held := start.Add(8 * time.Minute)

	t.Run("the hold does not lengthen the window", func(t *testing.T) {
		results := []agentResult{
			{arrivedAt: start.Add(200 * time.Millisecond)},
			{arrivedAt: start.Add(410 * time.Millisecond)},
			{arrivedAt: start.Add(90 * time.Millisecond)},
		}
		assert.Equal(t, 410*time.Millisecond, arrivalWindow(results, start),
			"the window ends at the last arrival, not at the end of the hold that follows it")
		assert.NotEqual(t, held.Sub(start), arrivalWindow(results, start))
	})

	t.Run("a machine that never arrived does not extend the window", func(t *testing.T) {
		results := []agentResult{
			{arrivedAt: start.Add(120 * time.Millisecond)},
			{err: errors.New("dial: timeout")},
		}
		assert.Equal(t, 120*time.Millisecond, arrivalWindow(results, start),
			"a failed machine has no arrival, so it cannot be the last one")
	})

	t.Run("a fleet where nobody arrived has no window", func(t *testing.T) {
		results := []agentResult{{err: errors.New("dial: timeout")}}
		assert.Zero(t, arrivalWindow(results, start),
			"no arrival is no window; a zero denominator is not a fast run")
	})

	t.Run("the reported window is the arrival, not the run", func(t *testing.T) {
		results := []agentResult{{arrivedAt: start.Add(350 * time.Millisecond)}}
		out := captureStdout(t, func() {
			reportResults(results, start, held.Sub(start), 1, nil)
		})
		assert.Contains(t, out, "Arrival window:",
			"the summarizer divides by this line; without it the rate has no denominator to use")
		assert.Contains(t, out, "350ms")
		assert.NotContains(t, out, "Arrival window:  8m0s")
	})
}

// captureStdout runs fn with os.Stdout redirected and returns what it printed.
// The results block is the harness's wire format with the summarizer, so what
// it prints is the thing under test.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	require.NoError(t, err)
	saved := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = saved }()

	fn()
	require.NoError(t, write.Close())
	out, err := io.ReadAll(read)
	require.NoError(t, err)
	return string(out)
}

// The register line the summarizer reads has to be registration.
//
// The harness's own clock around the register frame stops at a local send
// buffer, which cannot move however slow the write behind it becomes: that
// number published a p95 of zero on thirteen of eighteen nights, with the
// occasional scheduling spike, while two gate ceilings sat on it. The server
// times the same work where the device row lands, and that is the figure the
// row carries.
func TestTheRegisterLineCarriesTheServersFigureNotTheLocalWrite(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	results := []agentResult{
		// A local send buffer accepts a frame in microseconds, so the harness's
		// own register timings round to nothing.
		{registerDur: 12 * time.Microsecond, arrivedAt: start.Add(100 * time.Millisecond)},
		{registerDur: 9 * time.Microsecond, arrivedAt: start.Add(140 * time.Millisecond)},
	}

	t.Run("the server's histogram is what is printed", func(t *testing.T) {
		reading, err := ParseServerRegistration(sampleMetricsPage)
		require.NoError(t, err)
		require.True(t, reading.Measured())

		out := captureStdout(t, func() {
			reportResults(results, start, time.Minute, len(results), &reading)
		})
		line := resultsLine(t, out, "Register:")
		require.NotEmpty(t, line, "a run the server answered must publish a register line")
		assert.NotContains(t, line, "p50=0s",
			"a figure that cannot move is what this line stopped being")
		assert.Contains(t, line, "p95=")
		assert.Contains(t, line, "p99=")

		// Connect and handshake stay the harness's own: they are the generator's
		// side of the wire, and it is the only side that can see them.
		assert.NotEmpty(t, resultsLine(t, out, "Connect:"))
		assert.NotEmpty(t, resultsLine(t, out, "Handshake:"))
	})

	t.Run("a run the server did not answer publishes no register line", func(t *testing.T) {
		out := captureStdout(t, func() {
			reportResults(results, start, time.Minute, len(results), nil)
		})
		assert.Empty(t, resultsLine(t, out, "Register:"),
			"an absent figure is honest; the local write under registration's name is not")
	})

	t.Run("a server that saw no registration publishes no register line", func(t *testing.T) {
		out := captureStdout(t, func() {
			reportResults(results, start, time.Minute, len(results), &ServerRegistration{})
		})
		assert.Empty(t, resultsLine(t, out, "Register:"),
			"a run that measured nothing must not read as a run that measured zero milliseconds")
	})
}

// resultsLine returns the results-block line starting with prefix, or "".
func resultsLine(t *testing.T, out, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
