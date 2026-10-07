package protocol

import (
	"bytes"
	"compress/flate"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

func TestDecodeAlertEvidenceReadsWhatTheAgentWrote(t *testing.T) {
	t.Parallel()
	evidence, err := DecodeAlertEvidence(readGolden(t, "alert_evidence.bin"), EvidenceCodec)
	require.NoError(t, err)

	assert.Len(t, evidence.Ranked, evidenceRankedDims)
	assert.Len(t, evidence.Series, evidenceSeriesDims)
	assert.Len(t, evidence.Processes, evidenceProcessRows)
	assert.Len(t, evidence.LogSamples, evidenceLogSamples)
	assert.False(t, evidence.Truncated)

	for i := 1; i < len(evidence.Ranked); i++ {
		assert.LessOrEqual(t, evidence.Ranked[i].Score, evidence.Ranked[i-1].Score)
	}
}

func TestDecodeAlertEvidenceRefusesACodecItDoesNotKnow(t *testing.T) {
	t.Parallel()
	blob := readGolden(t, "alert_evidence.bin")

	for _, codec := range []string{"", "brotli-9", "deflate-2", "DEFLATE-1"} {
		_, err := DecodeAlertEvidence(blob, codec)
		assert.ErrorIsf(t, err, ErrUnknownEvidenceCodec, "codec %q must be refused by name", codec)
	}
}

func TestDecodeAlertEvidenceRefusesWhatDoesNotReadBack(t *testing.T) {
	t.Parallel()
	whole := readGolden(t, "alert_evidence.bin")

	flipped := bytes.Clone(whole)
	flipped[len(flipped)/2] ^= 0xff

	for _, tc := range []struct {
		name string
		blob []byte
	}{
		{"nothing at all", nil},
		{"cut in half", whole[:len(whole)/2]},
		{"a flipped byte", flipped},
		{"not compressed at all", []byte("ranked: none of your business")},
		{"compressed, but not evidence", deflated(t, []byte("not msgpack"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeAlertEvidence(tc.blob, EvidenceCodec)
			assert.Error(t, err)
			assert.NotErrorIs(t, err, ErrUnknownEvidenceCodec,
				"a blob that does not read back is a different failure from a codec nobody knows")
		})
	}
}

func TestDecodeAlertEvidenceRefusesABlobThatExpandsTooFar(t *testing.T) {
	t.Parallel()
	bomb := deflated(t, make([]byte, MaxEvidenceInflatedBytes+1))
	require.Less(t, len(bomb), MaxEvidenceBytes, "the refusal must be about what it expands to")

	_, err := DecodeAlertEvidence(bomb, EvidenceCodec)
	assert.ErrorIs(t, err, ErrEvidenceTooLarge)
}

func TestDecodedEvidenceSurvivesARoundTrip(t *testing.T) {
	t.Parallel()
	share := 37.5
	want := AlertEvidence{
		Ranked: []RankedDim{{Dim: "disk.await_ms", Score: 0.91}, {Dim: "cpu.iowait", Score: 0.62}},
		Series: []EvidenceSeries{{Dim: "disk.await_ms", Points: []HistoryPoint{{TS: 1, Value: 2}}}},
		Processes: []EvidenceProcess{
			{Rank: 1, Basename: "sqlservr", PID: 4218, CPUShare: &share, Mem: 121_634_816},
			{Rank: 2, Basename: "backup-agent", PID: 5120, Mem: 40_960},
		},
		LogSamples: []string{"controller reset"},
		Truncated:  true,
	}
	packed, err := msgpack.Marshal(want)
	require.NoError(t, err)

	got, err := DecodeAlertEvidence(deflated(t, packed), EvidenceCodec)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestEvidenceFromAnOlderAgentReadsWithoutAProcessorShare(t *testing.T) {
	t.Parallel()
	// An older agent writes its processor reading under "cpu", in units the share does not use.
	type olderProcessRow struct {
		Rank     uint32  `msgpack:"rank"`
		Basename string  `msgpack:"basename"`
		PID      uint32  `msgpack:"pid"`
		CPU      float64 `msgpack:"cpu"`
		Mem      float64 `msgpack:"mem"`
	}
	packed, err := msgpack.Marshal(map[string]any{
		"ranked":      []RankedDim{{Dim: "cpu.total", Score: 0.9}},
		"series":      []EvidenceSeries{},
		"processes":   []olderProcessRow{{Rank: 1, Basename: "backup-agent", PID: 4218, CPU: 2400, Mem: 122_052_608}},
		"log_samples": []string{},
		"truncated":   false,
	})
	require.NoError(t, err)

	evidence, err := DecodeAlertEvidence(deflated(t, packed), EvidenceCodec)
	require.NoError(t, err)

	require.Len(t, evidence.Processes, 1)
	row := evidence.Processes[0]
	assert.Equal(t, "backup-agent", row.Basename)
	assert.Equal(t, uint32(4218), row.PID)
	assert.Nil(t, row.CPUShare, "a reading in other units is not shown as a share")
	assert.InDelta(t, 122_052_608.0, row.Mem, 0)
}

// deflated compresses bytes the way the agent does, which is how a case builds a
// blob that is well-formed at the codec layer and something else underneath.
func deflated(t *testing.T, raw []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	writer, err := flate.NewWriter(&out, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = writer.Write(raw)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return out.Bytes()
}
