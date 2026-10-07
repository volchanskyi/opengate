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

func TestHarnessEmitsEveryStoredDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	want := make([]string, len(golden.Dims))
	for i, dim := range golden.Dims {
		want[i] = dim.Name
	}

	assert.Equal(t, want, defaultMetricDimNames,
		"the harness must emit the same dimensions, in the same order, as a machine does")
}

func TestDefaultWindowCarriesEveryDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	window := buildDefaultMetricWindow(1700000260)

	require.Len(t, window.Dims, len(golden.Dims))
	for i, dim := range golden.Dims {
		assert.Equal(t, dim.Name, window.Dims[i].Name, "dim %d", i)
	}
}

func TestExtraWindowCarriesEveryDimension(t *testing.T) {
	golden := goldenHostMetricWindow(t)

	window := buildExtraMetricWindow(1700000260)

	require.Len(t, window.Dims, len(golden.Dims))
	for i, dim := range golden.Dims {
		assert.Equal(t, dim.Name, window.Dims[i].Name, "dim %d", i)
	}
}

func TestHealthSummaryUsesTheServerFamilyNames(t *testing.T) {
	assert.Equal(t, []string{"cpu", "mem", "disk", "net", "proc"}, defaultFamilies)

	summary := buildHealthSummary(1700000260)
	require.Len(t, summary.PerFamilyRates, len(defaultFamilies))
	for i, family := range defaultFamilies {
		assert.Equal(t, family, summary.PerFamilyRates[i].Family, "family %d", i)
	}
}

func TestTelemetryShapeStaysInsideTheSeriesCap(t *testing.T) {
	const vitalSeriesCap = 24

	series := len(defaultMetricDimNames) + 1 + len(defaultFamilies)

	assert.LessOrEqual(t, series, vitalSeriesCap,
		"a device emits %d series; the central budget is %d", series, vitalSeriesCap)
}
