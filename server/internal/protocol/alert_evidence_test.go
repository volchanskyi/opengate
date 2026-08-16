package protocol

import (
	"bytes"
	"compress/flate"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"
)

// Reading evidence back on the server side.
//
// Evidence is written once, on a machine, and read whenever somebody opens the
// incident it belongs to. Nothing can be fetched again, so the read has exactly
// two honest answers: this is what the machine sent, or this cannot be read and
// here is why. A third — bytes handed back under a codec nobody claimed, or a
// structure assembled from a blob that half-decoded — would put invented detail
// in front of a technician deciding what happened to a customer's machine.

// TestDecodeAlertEvidenceReadsWhatTheAgentWrote is the codec contract from the
// reading end: the agent compresses with pure-Rust DEFLATE and the server reads
// it with the standard library, so neither side carries a compression
// dependency.
func TestDecodeAlertEvidenceReadsWhatTheAgentWrote(t *testing.T) {
	t.Parallel()
	evidence, err := DecodeAlertEvidence(readGolden(t, "alert_evidence.bin"), EvidenceCodec)
	require.NoError(t, err)

	assert.Len(t, evidence.Ranked, evidenceRankedDims)
	assert.Len(t, evidence.Series, evidenceSeriesDims)
	assert.Len(t, evidence.Processes, evidenceProcessRows)
	assert.Len(t, evidence.LogSamples, evidenceLogSamples)
	assert.False(t, evidence.Truncated)

	// The ranking is the ranking — a technician reads the first line expecting
	// it to be the worst one.
	for i := 1; i < len(evidence.Ranked); i++ {
		assert.LessOrEqual(t, evidence.Ranked[i].Score, evidence.Ranked[i-1].Score)
	}
}

// TestDecodeAlertEvidenceRefusesACodecItDoesNotKnow. The codec travels on the
// row rather than being assumed, precisely so a later one is additive — and a
// reader that meets one it does not know has to say so rather than inflate the
// bytes and hand back whatever comes out.
func TestDecodeAlertEvidenceRefusesACodecItDoesNotKnow(t *testing.T) {
	t.Parallel()
	blob := readGolden(t, "alert_evidence.bin")

	for _, codec := range []string{"", "brotli-9", "deflate-2", "DEFLATE-1"} {
		_, err := DecodeAlertEvidence(blob, codec)
		assert.ErrorIsf(t, err, ErrUnknownEvidenceCodec, "codec %q must be refused by name", codec)
	}
}

// TestDecodeAlertEvidenceRefusesWhatDoesNotReadBack. Every one of these is a
// blob that exists and cannot be trusted, and each has to fail rather than
// produce a partial structure: evidence is the whole of what will ever be known
// about a moment, so half of it is worse than none.
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

// TestDecodeAlertEvidenceRefusesABlobThatExpandsTooFar. The composition is fixed
// — eight ranked dimensions, three series, ten processes, twenty log lines — so
// nothing honest approaches the bound. What it refuses is the dishonest blob:
// sixty-four kilobytes of DEFLATE can name gigabytes of output, and inflating it
// to find out would be the server doing the endpoint's bidding.
func TestDecodeAlertEvidenceRefusesABlobThatExpandsTooFar(t *testing.T) {
	t.Parallel()
	bomb := deflated(t, make([]byte, MaxEvidenceInflatedBytes+1))
	require.Less(t, len(bomb), MaxEvidenceBytes, "the refusal must be about what it expands to")

	_, err := DecodeAlertEvidence(bomb, EvidenceCodec)
	assert.ErrorIs(t, err, ErrEvidenceTooLarge)
}

// TestDecodedEvidenceSurvivesARoundTrip pins that what the decoder produces is
// the same evidence the encoder was given, field for field. A decoder that
// silently dropped a field would pass every "it decoded" assertion.
func TestDecodedEvidenceSurvivesARoundTrip(t *testing.T) {
	t.Parallel()
	want := AlertEvidence{
		Ranked:     []RankedDim{{Dim: "disk.await_ms", Score: 0.91}, {Dim: "cpu.iowait", Score: 0.62}},
		Series:     []EvidenceSeries{{Dim: "disk.await_ms", Points: []HistoryPoint{{TS: 1, Value: 2}}}},
		Processes:  []ProcessReportEntry{{Rank: 1, Basename: "sqlservr", PID: 4218, CPU: 42, Mem: 18}},
		LogSamples: []string{"controller reset"},
		Truncated:  true,
	}
	packed, err := msgpack.Marshal(want)
	require.NoError(t, err)

	got, err := DecodeAlertEvidence(deflated(t, packed), EvidenceCodec)
	require.NoError(t, err)
	assert.Equal(t, want, got)
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
