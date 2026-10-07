package protocol

import (
	"bytes"
	"compress/flate"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// The composition an alert's evidence is assembled at, mirrored on the agent.
const (
	evidenceRankedDims   = 8
	evidenceSeriesDims   = 3
	evidenceSeriesPoints = 512
	evidenceProcessRows  = 10
	evidenceLogSamples   = 20
)

func TestGoldenControlAgentAlert(t *testing.T) {
	msg := decodeControlFrame(t, "control_agent_alert.bin")

	assert.Equal(t, MsgAgentAlert, msg.Type)
	assert.Equal(t, "0f1e2d3c-4b5a-6978-8796-a5b4c3d2e1f0", msg.AlertID)
	assert.Equal(t, "disk-latency-sustained", msg.RuleID)
	assert.Equal(t, uint32(3), msg.RuleVersion)
	require.NotNil(t, msg.Severity)
	assert.Equal(t, AlertSeverityCritical, *msg.Severity)
	assert.Equal(t, "disk.await_ms", msg.Metric)
	require.NotNil(t, msg.Value)
	assert.InDelta(t, 41.5, *msg.Value, 1e-9)
	assert.Equal(t, int64(1700000000), msg.WindowStartTS)
	assert.Equal(t, int64(1700000300), msg.WindowEndTS)
	assert.Equal(t, int64(1700000305), msg.ObservedTS)
	require.NotNil(t, msg.Backfilled)
	assert.False(t, *msg.Backfilled)
	assert.Equal(t, EvidenceCodec, msg.EvidenceCodec)
	assert.NotEmpty(t, msg.Evidence, "an alert's evidence must survive as bytes")
	assert.LessOrEqual(t, len(msg.Evidence), MaxEvidenceBytes)

	assertControlSurvivesReencode(t, msg)
}

func TestGoldenControlAgentAlertMinimal(t *testing.T) {
	// The smallest alert is a three-key map because both encoders drop empty fields.
	msg := decodeControlFrame(t, "control_agent_alert_min.bin")

	assert.Equal(t, MsgAgentAlert, msg.Type)
	require.NotNil(t, msg.Severity, "severity is always stated, never inferred")
	assert.Equal(t, AlertSeverityInfo, *msg.Severity)
	require.NotNil(t, msg.Backfilled)
	assert.False(t, *msg.Backfilled)
	assert.Empty(t, msg.AlertID)
	assert.Empty(t, msg.RuleID)
	assert.Zero(t, msg.RuleVersion)
	assert.Nil(t, msg.Value)
	assert.Empty(t, msg.Evidence)

	assertControlSurvivesReencode(t, msg)
}

func TestGoldenAlertEvidenceInflatesWithStdlib(t *testing.T) {
	blob := readGolden(t, "alert_evidence.bin")
	require.LessOrEqual(t, len(blob), MaxEvidenceBytes)

	packed, err := io.ReadAll(flate.NewReader(bytes.NewReader(blob)))
	require.NoError(t, err, "agent evidence must inflate with stdlib compress/flate")

	var evidence AlertEvidence
	require.NoError(t, msgpack.Unmarshal(packed, &evidence))

	assert.Len(t, evidence.Ranked, evidenceRankedDims)
	require.Len(t, evidence.Series, evidenceSeriesDims)
	for _, series := range evidence.Series {
		assert.NotEmpty(t, series.Dim)
		assert.LessOrEqual(t, len(series.Points), evidenceSeriesPoints)
	}
	require.Len(t, evidence.Processes, evidenceProcessRows)
	for i, row := range evidence.Processes {
		assert.Equal(t, uint32(1000+i), row.PID)
		assert.InDelta(t, float64(i)*1_048_576, row.Mem, 0, "memory travels in bytes")
		if i == evidenceProcessRows-1 {
			assert.Nil(t, row.CPUShare, "a row the device could not measure carries no share")
			continue
		}
		require.NotNil(t, row.CPUShare)
		assert.InDelta(t, float64(i)*2.5, *row.CPUShare, 1e-9)
	}
	assert.Len(t, evidence.LogSamples, evidenceLogSamples)
	assert.False(t, evidence.Truncated, "the shipped composition fits without truncation")

	// A technician reads the first line expecting the worst dimension.
	for i := 1; i < len(evidence.Ranked); i++ {
		assert.LessOrEqual(t, evidence.Ranked[i].Score, evidence.Ranked[i-1].Score,
			"ranked dimensions must arrive most anomalous first")
	}
}

// assertControlSurvivesReencode asserts a re-encoded control frame decodes to the same message;
// integer widths differ from rmp-serde's, so values are compared, not bytes.
func assertControlSurvivesReencode(t *testing.T, msg *ControlMessage) {
	t.Helper()
	reencoded, err := msgpack.Marshal(msg)
	require.NoError(t, err)

	var round ControlMessage
	require.NoError(t, msgpack.Unmarshal(reencoded, &round))
	assert.Equal(t, *msg, round, "no field may be lost when the server re-emits an alert")
}
