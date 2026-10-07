package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func completeBundle() *Bundle {
	start := time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)
	return &Bundle{
		SchemaVersion: bundleSchemaVersion,
		Run: RunIdentity{
			ID:             "run-1",
			Commit:         "deadbeef",
			ProfileName:    "normal",
			ProfileVersion: 1,
			Family:         FamilyNormal,
			Environment:    EnvStaging,
			StartedAt:      start,
			FinishedAt:     start.Add(6 * time.Minute),
		},
		Target: Fingerprint{
			Kind:        "staging",
			Description: "opengate-staging-server",
			CPUs:        0.25,
			MemoryBytes: 402_653_184,
		},
		Generator: Fingerprint{
			Kind:        "in-cluster-pod",
			Description: "k6 v1.6.1",
			CPUs:        1,
			MemoryBytes: 2_147_483_648,
			DiskBytes:   15_032_385_536,
		},
		Fixture: FixtureCounts{
			Size: FixtureSmall, Tenants: 1, Customers: 5, Sites: 10,
			Users: 20, Devices: 500,
		},
		Phases: []PhaseResult{{
			Name:                             "steady",
			StartedAt:                        start,
			FinishedAt:                       start.Add(5 * time.Minute),
			OfferedAgentArrivalsPerSecond:    5,
			AchievedAgentArrivalsPerSecond:   4.9,
			OfferedOperatorArrivalsPerSecond: 5,
			OfferedConnectedAgents:           500,
			AchievedConnectedAgents:          500,
			ErrorRate:                        0.001,
			ExpectedRejections:               12,
			Faults:                           0,
		}},
		Journeys: []JourneyResult{{
			Name: "device-list", Requests: 1200, ErrorRate: 0, LatencyP95Ms: 88,
		}},
		Observations: []Observation{{
			At: start.Add(time.Minute), Series: "agents_connected", Value: 500,
		}},
		GeneratorHeadroom: Headroom{Measured: true, CPUHeadroomPercent: 55, MemoryUsedPercent: 40},
		Cleanup:           CleanupProof{Verified: true},
		Verdict:           Verdict{Result: ResultValid},
	}
}

func bundleKeys(t *testing.T, b *Bundle) map[string]any {
	t.Helper()
	data, err := json.Marshal(b)
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(data, &generic))
	return generic
}

func TestCompleteBundleIsAccepted(t *testing.T) {
	require.NoError(t, completeBundle().Validate())
}

func TestBundleRefusesAMissingMandatorySection(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Bundle)
		wantErr string
	}{
		{"no schema version", func(b *Bundle) { b.SchemaVersion = 0 }, "schema_version"},
		{"no run id", func(b *Bundle) { b.Run.ID = "" }, "run.id"},
		{"no source revision", func(b *Bundle) { b.Run.Commit = "" }, "run.commit"},
		{"no profile named", func(b *Bundle) { b.Run.ProfileName = "" }, "run.profile_name"},
		{"no profile version", func(b *Bundle) { b.Run.ProfileVersion = 0 }, "run.profile_version"},
		{"no start time", func(b *Bundle) { b.Run.StartedAt = time.Time{} }, "run.started_at"},
		{"finished before it started", func(b *Bundle) { b.Run.FinishedAt = b.Run.StartedAt.Add(-time.Hour) }, "run.finished_at"},
		{"no target fingerprint", func(b *Bundle) { b.Target = Fingerprint{} }, "target"},
		{"no generator fingerprint", func(b *Bundle) { b.Generator = Fingerprint{} }, "generator"},
		{"no fixture counts", func(b *Bundle) { b.Fixture = FixtureCounts{} }, "fixture"},
		{"no phases", func(b *Bundle) { b.Phases = nil }, "phases"},
		{"a phase with no name", func(b *Bundle) { b.Phases[0].Name = "" }, "name"},
		{"no observations", func(b *Bundle) { b.Observations = nil }, "observations"},
		{"no cleanup proof and no reason", func(b *Bundle) { b.Cleanup = CleanupProof{} }, "cleanup"},
		{"no verdict", func(b *Bundle) { b.Verdict.Result = "" }, "verdict"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := completeBundle()
			tc.mutate(b)
			err := b.Validate()
			require.Error(t, err, "a bundle with %s must fail the run", tc.name)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestBundleKeepsOfferedAndAchievedApart(t *testing.T) {
	b := completeBundle()
	b.Phases[0].AchievedAgentArrivalsPerSecond = 2.0

	require.NoError(t, b.Validate())
	assert.InDelta(t, 5.0, b.Phases[0].OfferedAgentArrivalsPerSecond, 0)
	assert.InDelta(t, 2.0, b.Phases[0].AchievedAgentArrivalsPerSecond, 0)
	assert.Less(t, b.Phases[0].AchievedFraction(), 0.5)
}

func TestAttainmentReadsWhicheverSideWasMeasured(t *testing.T) {
	b := completeBundle()
	assert.InDelta(t, 0.98, b.Phases[0].AchievedFraction(), 0.001,
		"with no technician reading, the machines the phase drove are the attainment")

	measured := 1.0
	b.Phases[0].AchievedOperatorArrivalsPerSecond = &measured
	assert.InDelta(t, 0.2, b.Phases[0].AchievedFraction(), 0.001,
		"a measured technician rate is what the phase offered and is read first")
}

func TestExpectedRejectionsAreNotFaults(t *testing.T) {
	b := completeBundle()
	b.Phases[0].ExpectedRejections = 500
	b.Phases[0].Faults = 0

	require.NoError(t, b.Validate())
	assert.Zero(t, b.Phases[0].Faults)
	assert.EqualValues(t, 500, b.Phases[0].ExpectedRejections)
}

func TestBundleRefusesResidue(t *testing.T) {
	cases := map[string]func(*CleanupProof){
		"accounts":  func(c *CleanupProof) { c.OrphanUsers = 81 },
		"machines":  func(c *CleanupProof) { c.OrphanDevices = 40 },
		"customers": func(c *CleanupProof) { c.OrphanOrganizations = 8 },
		"sites":     func(c *CleanupProof) { c.OrphanSites = 38 },
	}
	for kind, leave := range cases {
		t.Run(kind, func(t *testing.T) {
			b := completeBundle()
			leave(&b.Cleanup)

			err := b.Validate()
			require.Error(t, err, "a run that left %s behind is not clean", kind)
			assert.Contains(t, err.Error(), "residue")
		})
	}
}

func TestAnUncountedCleanupIsReadableOnlyWithItsReason(t *testing.T) {
	b := completeBundle()
	b.Cleanup = CleanupProof{NotCounted: "the stack is torn down with the job that built it"}
	require.NoError(t, b.Validate())

	b.Cleanup = CleanupProof{}
	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup")
}

func TestBundleRoundTripsThroughDisk(t *testing.T) {
	dir := t.TempDir()
	b := completeBundle()

	path, err := b.WriteTo(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "bundle.json"), path)

	read, err := LoadBundle(path)
	require.NoError(t, err)
	assert.Equal(t, b.Run.ID, read.Run.ID)
	assert.Equal(t, b.Phases[0].Name, read.Phases[0].Name)
	assert.NoError(t, read.Validate())
}

func TestBundleRefusesToWriteWhenIncomplete(t *testing.T) {
	b := completeBundle()
	b.Phases = nil

	_, err := b.WriteTo(t.TempDir())
	require.Error(t, err)
}

func TestBundleFieldNamesAreStable(t *testing.T) {
	generic := bundleKeys(t, completeBundle())

	for _, key := range []string{
		"schema_version", "run", "target", "generator", "fixture",
		"phases", "journeys", "observations", "generator_headroom",
		"cleanup", "verdict",
	} {
		assert.Contains(t, generic, key, "bundle must carry %q at its top level", key)
	}
}

func TestLoadBundleNamesAMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bundle.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	_, err := LoadBundle(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode bundle")
}

func TestABundleCarriesWhatTheRunFiled(t *testing.T) {
	filed := 500
	b := completeBundle()
	b.Fixture.FiledDevices = &filed

	require.NoError(t, b.Validate())
	require.NotNil(t, b.Fixture.FiledDevices)
	assert.Equal(t, 500, *b.Fixture.FiledDevices)
}

func TestARunWithNoFixtureCarriesNoFilingCount(t *testing.T) {
	b := completeBundle()
	require.NoError(t, b.Validate())
	assert.Nil(t, b.Fixture.FiledDevices)
}

func TestABundleCarriesWhatTheRunWasRefused(t *testing.T) {
	b := completeBundle()
	b.Refusals = &RefusalCount{Requests: 115_228, Refused: 12}

	path, err := b.WriteTo(t.TempDir())
	require.NoError(t, err)

	read, err := LoadBundle(path)
	require.NoError(t, err)
	require.NotNil(t, read.Refusals, "the reading must survive a round trip, or the merge writes a key nothing reads")
	assert.Equal(t, int64(115_228), read.Refusals.Requests)
	assert.Equal(t, int64(12), read.Refusals.Refused)

	assert.Contains(t, bundleKeys(t, read), "refusals", "the merge writes this key, so the schema names it")
}

func TestABundleWithNoGeneratorDeclaresNoRefusalReading(t *testing.T) {
	b := completeBundle()
	require.Nil(t, b.Refusals)

	assert.NotContains(t, bundleKeys(t, b), "refusals",
		"a run that took no such reading must not report nought requests refused")
}
