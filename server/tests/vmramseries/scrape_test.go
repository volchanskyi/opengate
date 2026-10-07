package vmramseries

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// promSample is one exposition line: the raw label block between the braces, and the value.
type promSample struct {
	labels string
	value  float64
}

// promSamples returns every sample of one metric family, skipping comments and unparsable lines.
func promSamples(body, name string) []promSample {
	var out []promSample
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cut := strings.IndexAny(line, "{ ")
		if cut < 0 || line[:cut] != name {
			continue
		}

		labels, rest := "", line[cut:]
		if line[cut] == '{' {
			end := strings.LastIndex(line, "}")
			if end < 0 {
				continue
			}
			labels, rest = line[cut+1:end], line[end+1:]
		}
		// A trailing timestamp is optional; the value is the first field after the label block.
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		value, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			continue
		}
		out = append(out, promSample{labels: labels, value: value})
	}
	return out
}

// promGauge returns the value of a single-sample family, and whether the scrape carried it.
func promGauge(body, name string) (float64, bool) {
	samples := promSamples(body, name)
	if len(samples) != 1 {
		return 0, false
	}
	return samples[0].value, true
}

// promSum totals every sample of a family, such as on-disk bytes split across storage parts.
func promSum(body, name string) float64 {
	var total float64
	for _, sample := range promSamples(body, name) {
		total += sample.value
	}
	return total
}

var labelPattern = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_]*)="([^"]*)"`)

// promLabel returns a label's value from the first sample of a family.
func promLabel(body, name, label string) string {
	samples := promSamples(body, name)
	if len(samples) == 0 {
		return ""
	}
	for _, match := range labelPattern.FindAllStringSubmatch(samples[0].labels, -1) {
		if match[1] == label {
			return match[2]
		}
	}
	return ""
}

// exposition is a scrape with a version label, a process gauge, a split family and prefix-sharing
// neighbours.
const exposition = `# HELP vm_app_version version
# TYPE vm_app_version gauge
vm_app_version{version="victoria-metrics-20250506-000000-tags-v1.114.0-0-g0000000",short_version="v1.114.0"} 1
process_resident_memory_bytes 1.34217728e+08
process_resident_memory_peak_bytes 2.0e+08
vm_data_size_bytes{type="storage/inmemory"} 1024
vm_data_size_bytes{type="storage/small"} 2048
vm_data_size_bytes{type="indexdb/inmemory"} 512
vm_rows_added_to_storage_total 288000
vm_free_disk_space_bytes{path="/victoria-metrics-data"} 5.0e+10
`

func TestPromGauge(t *testing.T) {
	tests := []struct {
		name    string
		metric  string
		want    float64
		present bool
	}{
		{name: "reads a bare gauge", metric: "process_resident_memory_bytes", want: 1.34217728e+08, present: true},
		{name: "does not match a longer name sharing the prefix", metric: "process_resident_memory", present: false},
		{name: "reads a counter", metric: "vm_rows_added_to_storage_total", want: 288000, present: true},
		{name: "reports a family the scrape does not carry", metric: "vm_nonexistent_bytes", present: false},
		{name: "refuses a multi-sample family", metric: "vm_data_size_bytes", present: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := promGauge(exposition, tt.metric)
			require.Equal(t, tt.present, ok)
			require.InDelta(t, tt.want, got, 1e-9)
		})
	}
}

func TestPromSum(t *testing.T) {
	require.InDelta(t, 3584, promSum(exposition, "vm_data_size_bytes"), 1e-9,
		"on-disk bytes is the total across every storage part")
	require.InDelta(t, 0, promSum(exposition, "vm_nonexistent_bytes"), 1e-9)
}

func TestPromLabel(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		label  string
		want   string
	}{
		{name: "reads the build version", metric: "vm_app_version", label: "short_version", want: "v1.114.0"},
		{
			name: "reads a label whose value contains the delimiter", metric: "vm_app_version", label: "version",
			want: "victoria-metrics-20250506-000000-tags-v1.114.0-0-g0000000",
		},
		{name: "reports an absent label as empty", metric: "vm_app_version", label: "commit", want: ""},
		{name: "reports an absent family as empty", metric: "vm_nonexistent", label: "version", want: ""},
		{name: "reports a label on an unlabelled sample as empty", metric: "process_resident_memory_bytes", label: "type", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, promLabel(exposition, tt.metric, tt.label))
		})
	}
}

func TestPromSamplesIgnoresCommentsAndMalformedLines(t *testing.T) {
	const noisy = `# TYPE vm_data_size_bytes gauge
vm_data_size_bytes{type="storage/small"} not-a-number
vm_data_size_bytes{type="storage/big"
vm_data_size_bytes{type="storage/inmemory"} 64

`
	samples := promSamples(noisy, "vm_data_size_bytes")
	require.Len(t, samples, 1, "only the one parseable line is a sample")
	require.InDelta(t, 64, samples[0].value, 1e-9)
}

func TestVitalsExpositionIsOneLinePerSeries(t *testing.T) {
	const runID = "vmram-unit"
	devices := deviceIDs(runID, 3)

	body := vitalsExposition(runID, devices, 0)
	lines := strings.Split(strings.TrimSpace(body), "\n")
	require.Len(t, lines, len(devices)*seriesPerDevice())

	identities := make([]string, 0, len(lines))
	for _, line := range lines {
		identities = append(identities, line[:strings.LastIndex(line, "}")+1])
	}
	sort.Strings(identities)
	for i := 1; i < len(identities); i++ {
		require.NotEqual(t, identities[i-1], identities[i], "each line must create its own series")
	}
}

func TestVitalsExpositionBacktracksAtTheCadence(t *testing.T) {
	const runID = "vmram-unit"
	devices := deviceIDs(runID, 2)

	first := timestampsOf(t, vitalsExposition(runID, devices, 0))
	second := timestampsOf(t, vitalsExposition(runID, devices, 1))
	require.Len(t, first, len(devices)*seriesPerDevice())
	require.Len(t, second, len(first))

	for i := range first {
		require.Equal(t, first[i]-vitalsCadence.Milliseconds(), second[i],
			"sample index 1 sits exactly one cadence tick before index 0")
	}
}

func timestampsOf(t *testing.T, body string) []int64 {
	t.Helper()
	var out []int64
	for line := range strings.SplitSeq(strings.TrimSpace(body), "\n") {
		fields := strings.Fields(line)
		require.Len(t, fields, 3, "each line carries an identity, a value, and a timestamp")
		ts, err := strconv.ParseInt(fields[2], 10, 64)
		require.NoError(t, err)
		out = append(out, ts)
	}
	return out
}
