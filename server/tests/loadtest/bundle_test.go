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

// The bundle, not the metrics store, is what a run is. VictoriaMetrics keeps 30
// days; a comparison against a run older than that has to read something that
// still exists. So the bundle carries everything needed to interpret its own
// numbers — what produced them, what was offered, what was achieved, and what
// state the system was left in — and a bundle missing any of that fails the run
// rather than entering the trend as a thinner version of a real one.

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
			CPUs:        2,
			MemoryBytes: 9_874_911_232,
		},
		Generator: Fingerprint{
			Kind:        "in-cluster-pod",
			Description: "k6 v1.6.1",
			CPUs:        1,
			MemoryBytes: 402_653_184,
		},
		Fixture: FixtureCounts{
			Size: FixtureSmall, Tenants: 1, Customers: 5, Sites: 10,
			Users: 20, Devices: 500,
		},
		Phases: []PhaseResult{{
			Name:                      "steady",
			StartedAt:                 start,
			FinishedAt:                start.Add(5 * time.Minute),
			OfferedArrivalsPerSecond:  5,
			AchievedArrivalsPerSecond: 4.9,
			OfferedConnectedAgents:    500,
			AchievedConnectedAgents:   500,
			ErrorRate:                 0.001,
			ExpectedRejections:        12,
			Faults:                    0,
		}},
		Journeys: []JourneyResult{{
			Name: "device-list", Requests: 1200, ErrorRate: 0, LatencyP95Ms: 88,
		}},
		Observations: []Observation{{
			At: start.Add(time.Minute), Series: "agents_connected", Value: 500,
		}},
		GeneratorHeadroom: Headroom{CPUHeadroomPercent: 55, MemoryUsedPercent: 40},
		Cleanup:           CleanupProof{Verified: true, OrphanUsers: 0, OrphanDevices: 0, OrphanTenants: 0},
		Verdict:           Verdict{Result: ResultValid},
	}
}

func TestCompleteBundleIsAccepted(t *testing.T) {
	require.NoError(t, completeBundle().Validate())
}

// Each of these is a section whose absence changes what the numbers mean. A
// bundle without them is not a smaller bundle; it is a run nobody can read.
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
		{"no cleanup proof", func(b *Bundle) { b.Cleanup.Verified = false }, "cleanup"},
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

// Offered load and achieved load are separate fields because they answer
// different questions. Collapsing them hides the case the whole validity rule
// exists for: a generator that could not produce the load reads as a system
// that could not absorb it.
func TestBundleKeepsOfferedAndAchievedApart(t *testing.T) {
	b := completeBundle()
	b.Phases[0].AchievedArrivalsPerSecond = 2.0

	require.NoError(t, b.Validate())
	assert.InDelta(t, 5.0, b.Phases[0].OfferedArrivalsPerSecond, 0)
	assert.InDelta(t, 2.0, b.Phases[0].AchievedArrivalsPerSecond, 0)
	assert.Less(t, b.Phases[0].AchievedFraction(), 0.5)
}

// An expected rejection is the system working. Counting it as a fault makes a
// correctly enforced limit look like a defect and buries the real ones.
func TestExpectedRejectionsAreNotFaults(t *testing.T) {
	b := completeBundle()
	b.Phases[0].ExpectedRejections = 500
	b.Phases[0].Faults = 0

	require.NoError(t, b.Validate())
	assert.Zero(t, b.Phases[0].Faults)
	assert.EqualValues(t, 500, b.Phases[0].ExpectedRejections)
}

// A run leaves nothing behind. The proof travels with the run rather than being
// checked once and assumed thereafter.
func TestBundleRefusesResidue(t *testing.T) {
	b := completeBundle()
	b.Cleanup.OrphanUsers = 81

	err := b.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "residue")
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

// An invalid bundle never reaches disk in the first place: writing one is how
// it enters the trend.
func TestBundleRefusesToWriteWhenIncomplete(t *testing.T) {
	b := completeBundle()
	b.Phases = nil

	_, err := b.WriteTo(t.TempDir())
	require.Error(t, err)
}

// The bundle is JSON somebody else reads, so its field names are part of the
// contract. Renaming one silently breaks every reader.
func TestBundleFieldNamesAreStable(t *testing.T) {
	data, err := json.Marshal(completeBundle())
	require.NoError(t, err)

	var generic map[string]any
	require.NoError(t, json.Unmarshal(data, &generic))

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
