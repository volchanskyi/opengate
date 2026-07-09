package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// buildLogRateWindow produces an AgentMetricWindow carrying only log-rate dims
// with an empty org (the server assigns the authoritative org from the
// connection), mirroring the agent's WS-10 emission.
func TestBuildLogRateWindow(t *testing.T) {
	msg := buildLogRateWindow(1_700_000_000)

	assert.Equal(t, protocol.MsgAgentMetricWindow, msg.Type)
	assert.EqualValues(t, 1_700_000_000, msg.TS)
	assert.Empty(t, msg.OrgID, "agent must not assert an org; the server assigns it")
	require.NotEmpty(t, msg.Dims)
	for _, dim := range msg.Dims {
		assert.True(t, strings.HasPrefix(dim.Name, "log.rate."),
			"every dim must be a log-rate dim, got %q", dim.Name)
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
