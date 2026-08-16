package api

import (
	"bytes"
	"compress/flate"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// Reading the frozen evidence behind one alert.
//
// It is the one read that can answer three different ways, and the three have to
// stay distinguishable: here is what the machine sent, there is none, and this
// build cannot read what is stored. The third is what keeps a future codec an
// additive change rather than a page full of nonsense.

// TestEvidenceIsDecodedByTheServer. The stored blob is DEFLATE around msgpack
// and the browser has neither, so the decode happens here — and a codec this
// build does not read is reported as such rather than handed back as bytes.
func TestEvidenceIsDecodedByTheServer(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	want := sampleEvidence()
	incident, alert := e.open(t, alerts.SeverityCritical, encodedEvidence(t, want))
	path := "/api/v1/investigations/" + incident.String() + "/alerts/" + alert.String() + "/evidence"

	w := doRequest(e.srv, http.MethodGet, path, e.token, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got AlertEvidence
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Ranked, len(want.Ranked))
	assert.Equal(t, want.Ranked[0].Dim, got.Ranked[0].Dim)
	require.Len(t, got.Series, 1)
	require.Len(t, got.Series[0].Points, 1)
	require.Len(t, got.Processes, 1)
	assert.Equal(t, "sqlservr", got.Processes[0].Basename)
	assert.Equal(t, want.LogSamples, got.LogSamples)
	assert.True(t, got.Truncated, "a truncated blob is served with the flag intact so the page can say so")

	// The response carries the evidence contract and nothing else: the read path
	// must not fold in anything the machine did not redact before sending.
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	assert.ElementsMatch(t,
		[]string{"ranked", "series", "processes", "log_samples", "truncated"},
		keysOf(fields))
}

// TestEvidenceRefusesWhatItCannotRead. Three different answers, because they
// are three different situations: an alert that carries none, a room that does
// not hold that alert, and a blob written by something this build cannot read.
func TestEvidenceRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, alert := e.open(t, alerts.SeverityCritical, nil)
	base := "/api/v1/investigations/" + incident.String() + "/alerts/"

	w := doRequest(e.srv, http.MethodGet, base+alert.String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "an alert that carries none has none to serve")

	w = doRequest(e.srv, http.MethodGet, base+uuid.New().String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// A blob stored under a codec nobody named is unreadable rather than
	// missing, and saying so is the whole point of carrying the codec.
	e.rewriteEvidence(t, alert, []byte("whatever this is"), "brotli-9")
	w = doRequest(e.srv, http.MethodGet, base+alert.String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
}

// sampleEvidence is one machine's account of why an alert fired, already
// redacted the way the agent redacts it before sending.
func sampleEvidence() protocol.AlertEvidence {
	return protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{
			{Dim: "disk.await_ms", Score: 0.91},
			{Dim: "cpu.iowait", Score: 0.62},
		},
		Series: []protocol.EvidenceSeries{
			{Dim: "disk.await_ms", Points: []protocol.HistoryPoint{{TS: 1755138060, Value: 412.5}}},
		},
		Processes:  []protocol.ProcessReportEntry{{Rank: 1, Basename: "sqlservr", PID: 4218, CPU: 62.5, Mem: 18.25}},
		LogSamples: []string{"controller reset on \\\\FS01\\backup (user:[REDACTED]@FS01)"},
		Truncated:  true,
	}
}

// encodedEvidence compresses evidence the way the agent does.
func encodedEvidence(t *testing.T, evidence protocol.AlertEvidence) []byte {
	t.Helper()
	packed, err := msgpack.Marshal(evidence)
	require.NoError(t, err)

	var out bytes.Buffer
	writer, err := flate.NewWriter(&out, flate.DefaultCompression)
	require.NoError(t, err)
	_, err = writer.Write(packed)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return out.Bytes()
}

// keysOf names the fields a response carried, which is how a case asserts that
// nothing beyond the contract came back.
func keysOf(fields map[string]json.RawMessage) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	return out
}
