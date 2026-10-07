package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// measuredRun is a run with every reading a bundle needs, so a case can break exactly one.
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
		Headroom: Headroom{Measured: true, Scope: headroomScopeGenerator, CPUHeadroomPercent: 72, MemoryUsedPercent: 18},
	}
}

func TestTheTargetFingerprintIsTheOneTheRunWasGiven(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	require.NoError(t, bundle.Validate())
	assert.InDelta(t, 0.5, bundle.Target.CPUs, 0.001)
	assert.EqualValues(t, 384<<20, bundle.Target.MemoryBytes)
}

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

func TestGeneratorHeadroomIsMeasured(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	assert.True(t, bundle.GeneratorHeadroom.Measured)
	assert.InDelta(t, 72.0, bundle.GeneratorHeadroom.CPUHeadroomPercent, 0.001)
	assert.InDelta(t, 18.0, bundle.GeneratorHeadroom.MemoryUsedPercent, 0.001)
}

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
		Headroom:          Headroom{Measured: true, Scope: headroomScopeGenerator, CPUHeadroomPercent: 3, MemoryUsedPercent: 97},
		Phases:            []PhaseResult{{Name: "connect"}},
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, verdict.Reasons[0], "measured the generator")
}

func TestABundleWithNoRealRevisionFailsValidation(t *testing.T) {
	b := completeBundle()
	b.Run.Commit = unknownCommit

	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run.commit")
}

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

func TestTheFixtureWeightReachesTheBundle(t *testing.T) {
	in := measuredRun()
	in.FixtureWeight = &FixtureWeight{DatabaseBytes: 1_398_101, Counts: FixtureWeightCounts{TelemetrySeries: 4_096}}
	bundle := buildRunBundle(in)

	assert.EqualValues(t, 1_398_101, bundle.Fixture.DatabaseBytes)
	assert.EqualValues(t, 4_096, bundle.Fixture.TelemetrySeries)
}

// The golden values are the weighing script's output in fixtures/fixture-weight.json.
const (
	goldenFixtureBytes    = 1_343_488
	goldenTelemetrySeries = 15_000
)

func TestFixtureWeightIsReadFromWhatTheWeighingScriptWrote(t *testing.T) {
	path := filepath.Join(repoRoot(t), "scripts", "tests", "fixtures", "fixture-weight.json")

	weight, err := LoadFixtureWeight(path)
	require.NoError(t, err)
	assert.EqualValues(t, goldenFixtureBytes, weight.DatabaseBytes)
	assert.EqualValues(t, goldenTelemetrySeries, weight.Counts.TelemetrySeries)
}

func TestTheHarnessNeverClaimsTheCleanupItDidNotCount(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	require.NoError(t, bundle.Validate(), "a bundle that says why nothing is counted is readable")
	assert.False(t, bundle.Cleanup.Verified)
	assert.Contains(t, bundle.Cleanup.NotCounted, "cleanup step")
}

func TestTheDisposableStackSaysWhyNothingIsCounted(t *testing.T) {
	in := measuredRun()
	in.Profile = &Profile{Name: "volume-500", SchemaVersion: profileSchemaVersion, Family: FamilyVolume, Environment: EnvRunner}
	bundle := buildRunBundle(in)

	assert.False(t, bundle.Cleanup.Verified)
	assert.Contains(t, bundle.Cleanup.NotCounted, "torn down")
}

func TestACleanupProofWithResidueComesOutUnclean(t *testing.T) {
	dir := t.TempDir()
	path, err := buildRunBundle(measuredRun()).WriteTo(dir)
	require.NoError(t, err)

	// The stub psql reports three users, two devices, one organization and four sites left behind.
	psql := filepath.Join(dir, "psql")
	require.NoError(t, os.WriteFile(psql, []byte(`#!/usr/bin/env bash
query=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -tAc) query="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$query" ] || { cat >/dev/null; exit 0; }
case "$query" in
  *"FROM users"*) echo 3 ;;
  *"FROM devices"*) echo 2 ;;
  *"FROM organizations"*) echo 1 ;;
  *"FROM sites"*) echo 4 ;;
esac
`), 0o700))
	proof := filepath.Join(dir, "cleanup.json")
	cleanup := exec.Command(filepath.Join(repoRoot(t), "scripts", "loadtest-cleanup.sh"), proof)
	cleanup.Env = append(os.Environ(), "LOADTEST_PSQL="+psql)
	_ = cleanup.Run() // The script fails on residue and still writes the proof the merge reads.

	merge := exec.Command(filepath.Join(repoRoot(t), "scripts", "loadtest-bundle-merge.sh"), path, "--cleanup", proof)
	out, err := merge.CombinedOutput()
	require.NoErrorf(t, err, "merging the proof failed: %s", out)

	merged, err := LoadBundle(path)
	require.NoError(t, err)
	assert.True(t, merged.Cleanup.Verified)
	assert.EqualValues(t, 3, merged.Cleanup.OrphanUsers)
	assert.EqualValues(t, 1, merged.Cleanup.OrphanOrganizations)
	assert.EqualValues(t, 4, merged.Cleanup.OrphanSites)
	err = merged.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "residue")
}

func TestTheGeneratorFingerprintCarriesTheRunnerShapeAndItsFreeDisk(t *testing.T) {
	bundle := buildRunBundle(measuredRun())

	assert.InDelta(t, 4, bundle.Generator.CPUs, 0.001)
	assert.EqualValues(t, 16_766_414_848, bundle.Generator.MemoryBytes)
	assert.EqualValues(t, 15_032_385_536, bundle.Generator.DiskBytes,
		"the room a run has is what is free, not how big the partition is")
}

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

func TestAnExportWithNoJourneysCarriesNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quiet.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"metrics":{}}`), 0o600))

	journeys, err := LoadJourneys(path)
	require.NoError(t, err)
	assert.Empty(t, journeys)
}

func TestABusyBoxSharedWithTheTargetDoesNotInvalidateTheRun(t *testing.T) {
	verdict := Classify(RunInputs{
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom:          Headroom{Measured: true, Scope: headroomScopeMachine, CPUHeadroomPercent: 0},
		Phases:            []PhaseResult{{Name: "connect"}},
	})

	assert.Equal(t, ResultValid, verdict.Result)
}

func TestAGeneratorRefusedTheProcessorInvalidatesTheRun(t *testing.T) {
	refused := 45.0
	verdict := Classify(RunInputs{
		ExpectedScenarios: []string{"quic-agents"},
		ProducedScenarios: []string{"quic-agents"},
		Headroom: Headroom{
			Measured: true, Scope: headroomScopeGenerator,
			CPUHeadroomPercent: 80, CPURefusedPercent: &refused,
		},
		Phases: []PhaseResult{{Name: "connect"}},
	})

	require.Equal(t, ResultInvalid, verdict.Result)
	assert.Contains(t, verdict.Reasons[0], "refused the processor")
}
