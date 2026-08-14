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

// The edge-first alert wire contract. An alert is the only thing that ever
// carries the detail behind a signal: the server holds no high-resolution
// history to go back to and has no way of asking the device later, so whatever
// is not on the message at fire time does not exist. These fixtures are
// Rust-encoded and decoded here, and every one of them is put back through the
// server's own encoder, because a field the server silently drops on the way
// through is a field the investigation never gets back.

// The composition an alert's evidence is assembled at. Stated here as well as on
// the agent so the two are asserted against each other rather than against
// whatever the fixture happens to hold.
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
	// The smallest alert an agent can emit. Both encoders drop what is empty, so
	// this is a three-key map — and it still has to decode, with severity read as
	// a stated value rather than an absent one.
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

// TestGoldenAlertEvidenceInflatesWithStdlib is the codec half of the contract:
// the agent compresses evidence with pure-Rust DEFLATE and the server reads it
// with compress/flate, so neither side pays for a compression dependency. The
// counts are the fixed composition — not "top-N by whatever fit" — because two
// incidents are only comparable if they were assembled the same way.
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
	assert.Len(t, evidence.Processes, evidenceProcessRows)
	assert.Len(t, evidence.LogSamples, evidenceLogSamples)
	assert.False(t, evidence.Truncated, "the shipped composition fits without truncation")

	// Ranked order is the ranking, not an accident of map iteration: a
	// technician reads the first line and expects it to be the worst one.
	for i := 1; i < len(evidence.Ranked); i++ {
		assert.LessOrEqual(t, evidence.Ranked[i].Score, evidence.Ranked[i-1].Score,
			"ranked dimensions must arrive most anomalous first")
	}
}

// assertControlSurvivesReencode re-encodes a decoded control frame and decodes
// it again, asserting the message is unchanged. A field the server can read but
// cannot write — the shape that loses evidence in transit while every decode
// assertion still passes — shows up here and nowhere else.
//
// Byte equality with the agent's own encoding is deliberately not the assertion:
// the two integer policies differ on purpose. rmp-serde writes an integer in the
// fewest bytes that hold it, while the server's encoder is width-preserving so
// that it stays byte-identical to its own reflection encoder, which
// codec_wire_equivalence_test.go pins. Both are valid msgpack for the same
// value, so the contract that matters across the languages is that the value
// survives.
func assertControlSurvivesReencode(t *testing.T, msg *ControlMessage) {
	t.Helper()
	reencoded, err := msgpack.Marshal(msg)
	require.NoError(t, err)

	var round ControlMessage
	require.NoError(t, msgpack.Unmarshal(reencoded, &round))
	assert.Equal(t, *msg, round, "no field may be lost when the server re-emits an alert")
}
