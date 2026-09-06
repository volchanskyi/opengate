package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the bundle says about the two sides of the measurement, the room the
// generator had, and the fleet that actually existed.
//
// Each of these was a literal. A latency figure is a property of the pair, so a
// target reported as one processor and one byte of memory makes every number
// beside it uninterpretable — and four bundles from a sweep whose whole subject
// was the processor count all said the same one.

// measuredRun is a run with every reading a bundle needs, so a case can break
// exactly one of them.
func measuredRun() runBundleInputs {
	return runBundleInputs{
		Results:    harnessResults(),
		StartedAt:  time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC),
		Total:      3 * time.Second,
		AgentCount: 3,
		Target:     "opengate-staging-server:9090",
		Commit:     "0b5d1f2c3a4e5d6f7089abcdef0123456789abcd",
		TargetShape: Fingerprint{
			Kind:        "system-under-test",
			Description: "compose stack, 0.5 processors",
			CPUs:        0.5,
			MemoryBytes: 384 << 20,
		},
		GeneratorShape: Fingerprint{
			Kind:        "github-hosted-runner",
			Description: "ubuntu24",
			CPUs:        4,
			MemoryBytes: 16_766_414_848,
			DiskBytes:   15_032_385_536,
		},
		Headroom: Headroom{Measured: true, CPUHeadroomPercent: 72, MemoryUsedPercent: 18},
	}
}

// D8. The sweep hardcoded one processor and one byte for every leg, so four
// bundles whose only subject was the processor count reported the same target.
func TestTheTargetFingerprintIsTheOneTheRunWasGiven(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	require.NoError(t, bundle.Validate())
	assert.InDelta(t, 0.5, bundle.Target.CPUs, 0.001)
	assert.EqualValues(t, 384<<20, bundle.Target.MemoryBytes)
}

// The guard that keeps it from going back. One byte of memory is not a reading
// any machine could produce, so a bundle carrying it is carrying a placeholder.
func TestABundleWithAPlaceholderFingerprintFailsValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Bundle)
	}{
		{"target", func(b *Bundle) { b.Target = Fingerprint{Kind: "k", Description: "d", CPUs: 1, MemoryBytes: 1} }},
		{"generator", func(b *Bundle) { b.Generator = Fingerprint{Kind: "k", Description: "d", CPUs: 1, MemoryBytes: 1} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := completeBundle()
			tc.mutate(b)

			err := b.Validate()
			require.Error(t, err, "a fingerprint reporting one byte of memory is a literal, not a measurement")
			assert.Contains(t, err.Error(), "memory")
		})
	}
}

// D7. Generator headroom was written unconditionally as 100% free and 0% used,
// so the rule invalidating a run whose generator had nothing left was dead.
func TestGeneratorHeadroomIsMeasured(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	assert.True(t, bundle.GeneratorHeadroom.Measured)
	assert.InDelta(t, 72.0, bundle.GeneratorHeadroom.CPUHeadroomPercent, 0.001)
	assert.InDelta(t, 18.0, bundle.GeneratorHeadroom.MemoryUsedPercent, 0.001)
}

// A reading nobody took is not a reading of plenty. This is what makes the
// sweep's top rung — where the generator is squeezed onto the same four
// processors as the stack it drives — report its own starvation rather than
// answer wrongly.
func TestAnUnmeasuredGeneratorInvalidatesTheRun(t *testing.T) {
	verdict := Classify(RunInputs{
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{},
		Phases:            []PhaseResult{{Name: "connect"}},
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, verdict.Reasons[0], "generator was not measured")
}

func TestAStarvedGeneratorInvalidatesTheRun(t *testing.T) {
	verdict := Classify(RunInputs{
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{Measured: true, CPUHeadroomPercent: 3, MemoryUsedPercent: 97},
		Phases:            []PhaseResult{{Name: "connect"}},
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, verdict.Reasons[0], "measured the generator")
}

// D21. The harness runs inside a pod, which inherits no revision, so every
// staging bundle carried the string "unknown" and validation accepted it.
func TestABundleWithNoRealRevisionFailsValidation(t *testing.T) {
	b := completeBundle()
	b.Run.Commit = unknownCommit

	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run.commit")
}

// D18. The count was the plan rather than the fleet: a bundle said 2,000
// machines while the database, weighed in the same job, held 500.
func TestTheFixtureCountsMachinesThatEnrolled(t *testing.T) {
	plan, err := PlanFixture(FixtureLarge, 3)
	require.NoError(t, err)
	built := BuiltFixture{
		Size:           plan.Size,
		Customers:      []BuiltCustomer{{ID: "a"}, {ID: "b"}},
		Users:          []string{"one@x.invalid"},
		Sites:          9,
		PlannedDevices: plan.Devices,
	}

	in := measuredRun()
	in.Fixture = &built
	bundle := buildRunBundle(in)

	assert.Equal(t, 2, bundle.Fixture.Devices,
		"two of the three machines arrived, and the fleet is the machines that exist")
	assert.Equal(t, plan.Devices, bundle.Fixture.PlannedDevices,
		"what was asked for travels beside it under its own name")
	assert.NotEqual(t, bundle.Fixture.PlannedDevices, bundle.Fixture.Devices)
}

// D31. The volume family's whole finding is the fixture's weight, and the one
// job that measured it wrote the figure into a file the bundle never read.
func TestTheFixtureWeightReachesTheBundle(t *testing.T) {
	in := measuredRun()
	in.FixtureWeight = &FixtureWeight{DatabaseBytes: 1_398_101, TelemetrySeries: 4_096}
	bundle := buildRunBundle(in)

	assert.EqualValues(t, 1_398_101, bundle.Fixture.DatabaseBytes)
	assert.EqualValues(t, 4_096, bundle.Fixture.TelemetrySeries)
}

func TestFixtureWeightIsReadFromWhatTheJobWrote(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture-weight.json")
	require.NoError(t, os.WriteFile(path,
		[]byte(`{"fixture_bytes":1398101,"telemetry_series":4096}`), 0o600))

	weight, err := LoadFixtureWeight(path)
	require.NoError(t, err)
	assert.EqualValues(t, 1_398_101, weight.DatabaseBytes)
	assert.EqualValues(t, 4_096, weight.TelemetrySeries)
}

// D22. The runner's shape was measured into the job environment and read by
// nothing, and the one figure taken for the volume family answered the wrong
// question — the partition's total size rather than the room a fixture has.
func TestTheGeneratorFingerprintCarriesTheRunnerShapeAndItsFreeDisk(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	assert.InDelta(t, 4, bundle.Generator.CPUs, 0.001)
	assert.EqualValues(t, 16_766_414_848, bundle.Generator.MemoryBytes)
	assert.EqualValues(t, 15_032_385_536, bundle.Generator.DiskBytes,
		"the room a run has is what is free, not how big the partition is")
}

// D17. Three journeys are already timed by the technician-side generator and
// published into the trend, while the bundle beside them carried a null.
func TestJourneysReachTheBundle(t *testing.T) {
	in := measuredRun()
	in.Journeys = []JourneyResult{
		{Name: "device-list", Requests: 1200, ErrorRate: 0, LatencyP50Ms: 41, LatencyP95Ms: 88},
	}
	bundle := buildRunBundle(in)

	require.Len(t, bundle.Journeys, 1)
	assert.Equal(t, "device-list", bundle.Journeys[0].Name)
	assert.InDelta(t, 88.0, bundle.Journeys[0].LatencyP95Ms, 0.001)
}

// The journeys come out of the export the technician-side generator already
// writes, so nothing has to be measured twice or restated.
func TestJourneysAreReadFromTheGeneratorsOwnExport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "api-baseline.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
	  "metrics": {
	    "journey_device_list_ms": {"type": "trend", "values": {"med": 41.0, "p(95)": 88.5, "count": 1200}},
	    "journey_device_detail_ms": {"type": "trend", "values": {"med": 60.0, "p(95)": 140.25, "count": 600}},
	    "http_req_duration": {"type": "trend", "values": {"med": 7.0, "p(95)": 12.0, "count": 7228}}
	  }
	}`), 0o600))

	journeys, err := LoadJourneys(path)
	require.NoError(t, err)

	require.Len(t, journeys, 2, "only the named journeys, not every trend in the export")
	assert.Equal(t, "device-detail", journeys[0].Name)
	assert.Equal(t, "device-list", journeys[1].Name)
	assert.InDelta(t, 88.5, journeys[1].LatencyP95Ms, 0.001)
	assert.EqualValues(t, 1200, journeys[1].Requests)
}

// An export that carries no journeys is silence rather than an error: the
// technician-side generator does not run in every venue.
func TestAnExportWithNoJourneysCarriesNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quiet.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"metrics":{}}`), 0o600))

	journeys, err := LoadJourneys(path)
	require.NoError(t, err)
	assert.Empty(t, journeys)
}
