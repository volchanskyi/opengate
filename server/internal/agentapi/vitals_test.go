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

// The family vocabulary bounds the second half of a device's central series the
// same way the dim vocabulary bounds the first. It is pinned in full here
// because the count feeds anomalySeriesPerDevice, which feeds the cap.
func TestAnomalyFamiliesAreTheAgreedVocabulary(t *testing.T) {
	assert.Equal(t, []string{
		"cpu",
		"mem",
		"disk",
		"net",
		"proc",
	}, anomalyFamilies, "the health-summary family vocabulary")

	require.Len(t, anomalyFamilySet, len(anomalyFamilies), "no family is listed twice")
	for _, family := range anomalyFamilies {
		assert.True(t, anomalyFamilySet[family], "%s is in the set the ingest path checks", family)
	}
}

// The per-device series budget counts one rate per listed family, so the two
// must move together: a family added to the vocabulary spends a series.
func TestAnomalySeriesCountsTheFamilyVocabulary(t *testing.T) {
	assert.Equal(t, 1+len(anomalyFamilies), anomalySeriesPerDevice,
		"one node-wide rate plus one per family")
}

// An unlisted family name is dropped rather than written. Without this the
// family label would be agent-controlled, and one misbehaving agent could
// multiply a whole tenant's central series count.
func TestIsAnomalyFamilyFiltersUnlistedNames(t *testing.T) {
	assert.True(t, isAnomalyFamily("cpu"))
	assert.True(t, isAnomalyFamily("proc"))
	assert.True(t, isAnomalyFamily("net"))
	assert.False(t, isAnomalyFamily("process"), "the vocabulary spells the process family proc")
	assert.False(t, isAnomalyFamily("CPU"), "the vocabulary is case-sensitive")
	assert.False(t, isAnomalyFamily("gpu"), "a plausible-looking family is still unlisted")
	assert.False(t, isAnomalyFamily(""), "an empty family name is not a family")
	assert.False(t, isAnomalyFamily("attacker.family.0"))
}
