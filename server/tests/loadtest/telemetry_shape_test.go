package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A load harness only measures the ingest path if it sends what a machine
// sends. Emitting fewer dimensions than the server stores leaves those series
// unexercised — they cost cardinality and write time in production and nothing
// in the run — and emitting a family name the server does not know produces a
// series nothing reads.
//
// Both halves of the contract are pinned by the cross-language golden fixture,
// which is what the agent and the server already agree through, so this reads
// the same file rather than restating a list a third time.

// goldenHostMetricWindow decodes the committed host-metric window golden.
func goldenHostMetricWindow(t *testing.T) *protocol.ControlMessage {
	t.Helper()
	_, filename, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..",
		"testdata", "golden", "control_agent_metric_window_host_metrics.bin")
	data, err := os.ReadFile(filepath.Clean(path))
	require.NoError(t, err, "host-metric window golden missing at %s", path)

	codec := &protocol.Codec{}
	frameType, payload, err := codec.ReadFrame(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, protocol.FrameControl, frameType)
	msg, err := codec.DecodeControl(payload)
	require.NoError(t, err)
	return msg
}

// TestHarnessEmitsEveryStoredDimension proves the sampler shape the harness
// sends is the whole vocabulary the server stores, in the order a real window
// carries it. A dimension missing here is a dimension no load run ever writes.
func TestHarnessEmitsEveryStoredDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	want := make([]string, len(golden.Dims))
	for i, dim := range golden.Dims {
		want[i] = dim.Name
	}

	assert.Equal(t, want, defaultMetricDimNames,
		"the harness must emit the same dimensions, in the same order, as a machine does")
}

// TestDefaultWindowCarriesEveryDimension pins the built frame rather than the
// list behind it, so a window that silently drops dimensions on the way to the
// wire is caught too.
func TestDefaultWindowCarriesEveryDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	window := buildDefaultMetricWindow(1700000260)

	require.Len(t, window.Dims, len(golden.Dims))
	for i, dim := range golden.Dims {
		assert.Equal(t, dim.Name, window.Dims[i].Name, "dim %d", i)
	}
}

// TestExtraWindowCarriesEveryDimension — the multi-tenant stress window shares
// the same vocabulary, so it cannot drift away from the default one.
func TestExtraWindowCarriesEveryDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	window := buildExtraMetricWindow(1700000260)

	require.Len(t, window.Dims, len(golden.Dims))
	for i, dim := range golden.Dims {
		assert.Equal(t, dim.Name, window.Dims[i].Name, "dim %d", i)
	}
}

// TestHealthSummaryUsesTheServerFamilyNames pins the anomaly families to the
// names the server accounts for. A summary reporting "memory" and "network"
// writes two families the server never asked about and leaves "mem", "net" and
// "proc" with no sample at all.
func TestHealthSummaryUsesTheServerFamilyNames(t *testing.T) {
	assert.Equal(t, []string{"cpu", "mem", "disk", "net", "proc"}, defaultFamilies)

	summary := buildHealthSummary(1700000260)
	require.Len(t, summary.PerFamilyRates, len(defaultFamilies))
	for i, family := range defaultFamilies {
		assert.Equal(t, family, summary.PerFamilyRates[i].Family, "family %d", i)
	}
}

// TestTelemetryShapeStaysInsideTheSeriesCap keeps the harness honest about
// cardinality: one device's window plus its anomaly summary must fit the budget
// a device is allowed centrally, or the run proves a shape production refuses.
func TestTelemetryShapeStaysInsideTheSeriesCap(t *testing.T) {
	const vitalSeriesCap = 24

	series := len(defaultMetricDimNames) + 1 + len(defaultFamilies)

	assert.LessOrEqual(t, series, vitalSeriesCap,
		"a device emits %d series; the central budget is %d", series, vitalSeriesCap)
}
