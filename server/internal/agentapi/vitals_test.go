package agentapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The vocabulary is what bounds central cardinality, so it is pinned here in
// full. A device's series are the allowlisted dims plus the anomaly rates, and
// that total must stay under the cap with the headroom the platform-specific
// vitals are reserved for.
func TestVitalDimsAreTheAgreedVocabulary(t *testing.T) {
	assert.Equal(t, []string{
		"cpu.total",
		"cpu.total.max",
		"mem.used_percent",
		"mem.used_percent.max",
		"disk.used_percent",
		"net.rx_bps",
		"net.rx_bps.max",
		"net.tx_bps",
		"net.tx_bps.max",
		"disk.mounts_critical",
		"stall.cpu.some",
		"stall.mem.some",
		"stall.mem.full",
		"stall.io.some",
		"stall.io.full",
		"disk.await_ms",
		"disk.await_ms.max",
		"disk.queue_depth",
	}, vitalDims, "the metric-window vocabulary")

	require.Len(t, vitalDimSet, len(vitalDims), "no dim is listed twice")
	for _, dim := range vitalDims {
		assert.True(t, vitalDimSet[dim], "%s is in the set the ingest path checks", dim)
	}
}

func TestVitalSeriesPerDeviceFitTheCap(t *testing.T) {
	total := len(vitalDims) + anomalySeriesPerDevice
	assert.Equal(t, 24, total, "the vitals a Linux device emits today")
	assert.Equal(t, vitalSeriesCap, total,
		"a Linux device now occupies the whole cap, so the next vital re-opens it")
}

// An unlisted dim is dropped rather than written, which is what makes central
// cardinality a compile-time constant instead of an agent-controlled one.
func TestKnownVitalDimsFiltersUnlistedNames(t *testing.T) {
	assert.True(t, isVitalDim("cpu.total"))
	assert.True(t, isVitalDim("net.tx_bps.max"))
	assert.True(t, isVitalDim("stall.io.full"))
	assert.True(t, isVitalDim("disk.await_ms.max"))
	assert.False(t, isVitalDim("cpu.total.min"), "a plausible-looking name is still unlisted")
	assert.False(t, isVitalDim("disk.used_percent.max"), "the disk gauge ships no maximum")
	assert.False(t, isVitalDim("stall.cpu.full"), "the kernel defines CPU full as always zero")
	assert.False(t, isVitalDim("stall.io.some.max"), "a stall vital ships no maximum")
	assert.False(t, isVitalDim("disk.queue_depth.max"),
		"queue depth is already an average over the interval")
	assert.False(t, isVitalDim("disk.busy_percent"),
		"a utilization percentage saturates on parallel devices and is not a vital")
	assert.False(t, isVitalDim(""), "an empty dim name is not a dim")
	assert.False(t, isVitalDim("attacker.dim.0"))
}
