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
	require.Len(t, got.Processes, 2)
	assert.Equal(t, "sqlservr", got.Processes[0].Basename)
	require.NotNil(t, got.Processes[0].Cpu)
	assert.InDelta(t, 62.5, *got.Processes[0].Cpu, 1e-9)
	assert.InDelta(t, 121_634_816.0, got.Processes[0].Mem, 0)
	assert.Nil(t, got.Processes[1].Cpu, "a process the agent could not measure serves no share")
	assert.Contains(t, w.Body.String(), `"cpu":null`)
	assert.Equal(t, want.LogSamples, got.LogSamples)
	assert.True(t, got.Truncated, "a truncated blob is served with the flag intact so the page can say so")

	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &fields))
	assert.ElementsMatch(t,
		[]string{"ranked", "series", "processes", "log_samples", "truncated"},
		keysOf(fields))
}

func TestEvidenceRefusesWhatItCannotRead(t *testing.T) {
	t.Parallel()
	e := newInvestigations(t, stubRuleCoverage{})
	incident, alert := e.open(t, alerts.SeverityCritical, nil)
	base := "/api/v1/investigations/" + incident.String() + "/alerts/"

	w := doRequest(e.srv, http.MethodGet, base+alert.String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code, "an alert that carries none has none to serve")

	w = doRequest(e.srv, http.MethodGet, base+uuid.New().String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// A blob under an unknown codec answers 422, distinct from a missing alert's 404.
	e.rewriteEvidence(t, alert, []byte("whatever this is"), "brotli-9")
	w = doRequest(e.srv, http.MethodGet, base+alert.String()+"/evidence", e.token, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
}

// sampleEvidence returns evidence already redacted the way the agent redacts it before sending.
func sampleEvidence() protocol.AlertEvidence {
	share := 62.5
	return protocol.AlertEvidence{
		Ranked: []protocol.RankedDim{
			{Dim: "disk.await_ms", Score: 0.91},
			{Dim: "cpu.iowait", Score: 0.62},
		},
		Series: []protocol.EvidenceSeries{
			{Dim: "disk.await_ms", Points: []protocol.HistoryPoint{{TS: 1755138060, Value: 412.5}}},
		},
		Processes: []protocol.EvidenceProcess{
			{Rank: 1, Basename: "sqlservr", PID: 4218, CPUShare: &share, Mem: 121_634_816},
			{Rank: 2, Basename: "backup-agent", PID: 5120, Mem: 40_960},
		},
		LogSamples: []string{"controller reset on \\\\FS01\\backup (user:[REDACTED]@FS01)"},
		Truncated:  true,
	}
}

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

func keysOf(fields map[string]json.RawMessage) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	return out
}
