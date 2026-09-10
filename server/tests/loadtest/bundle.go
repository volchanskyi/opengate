package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The evidence bundle is what a run is. The metrics store keeps thirty days, so
// any comparison older than that has to read something that still exists —
// which makes the bundle authoritative and the dashboard a view of it.
//
// A bundle therefore carries enough to interpret its own numbers without the
// system that produced them: what produced them, on what, against how much
// data, what load was asked for, what load actually arrived, what the system
// did, and what state it was left in. A section missing from that list does not
// make the bundle smaller — it makes the run unreadable, so it fails the run.

// bundleSchemaVersion is the shape of the document below. It travels inside the
// document because a trend that silently spans two meanings of a field is worse
// than one with a gap in it.
//
// Version 2 separates the two sides of the arrival rate. A phase used to carry
// one pair of arrival fields whose offered half was technician load and whose
// achieved half was the same figure copied across, which made the pair
// unreadable in both directions: the units belonged to a generator this process
// does not drive, and the ratio between them was one by construction.
//
// Version 3 makes three of the numbers above readings. A phase's achieved
// arrivals are counted where a machine arrives rather than where its life ends,
// so a fleet held to the end of the walk no longer reports none; a phase's
// boundaries are the clock rather than its own declaration; and the generator's
// room is bracketed around the load and says whose room it is — its own
// allowance, or a box it shares with the system under test.
//
// Version 4 adds how hard the target worked. A phase carried the wait times it
// saw and nothing about what the target did with the allowance it was given, so
// a server out of processor and a server idle but slow read identically — and
// every statement about which of the two a night showed was an inference.
const bundleSchemaVersion = 4

// bundleFileName is what a bundle directory holds.
const bundleFileName = "bundle.json"

// RunIdentity says which run this is and what produced it.
type RunIdentity struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
	// ProfileName and ProfileVersion together say what was asked for. Both
	// travel because a profile is edited in place.
	ProfileName    string      `json:"profile_name"`
	ProfileVersion int         `json:"profile_version"`
	Family         Family      `json:"family"`
	Environment    Environment `json:"environment"`
	StartedAt      time.Time   `json:"started_at"`
	FinishedAt     time.Time   `json:"finished_at"`
}

// Fingerprint describes one side of the measurement. Both sides are recorded
// because a latency figure is a property of the pair, not of the target: the
// same server measured from a starved generator is a different number.
type Fingerprint struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	// CPUs is fractional because a container's share of a machine is. The
	// scaling sweep's whole subject is this number, and half a processor is one
	// of its rungs.
	CPUs        float64 `json:"cpus"`
	MemoryBytes int64   `json:"memory_bytes"`
	// DiskBytes is the room this side has, which for a machine that builds a
	// stack and a fixture on it is what is free rather than how big the
	// partition is. Zero means it was not measured.
	DiskBytes int64  `json:"disk_bytes,omitempty"`
	Arch      string `json:"arch,omitempty"`
}

// FixtureCounts is how much data was already there. It is a count rather than a
// size claim, plus the measured on-disk weight where the run took one.
type FixtureCounts struct {
	Size      FixtureSize `json:"size"`
	Tenants   int         `json:"tenants"`
	Customers int         `json:"customers"`
	Sites     int         `json:"sites"`
	Users     int         `json:"users"`
	// Devices is the fleet that exists: the machines that enrolled. PlannedDevices
	// is what the plan asked for, and the two are different numbers — a bundle
	// reported two thousand machines while the database, weighed in the same
	// job, held five hundred.
	Devices        int `json:"devices"`
	PlannedDevices int `json:"planned_devices,omitempty"`
	// DatabaseBytes and TelemetrySeries are filled by a run that weighed the
	// fixture. Zero means it was not measured, which is different from empty.
	DatabaseBytes   int64 `json:"database_bytes,omitempty"`
	TelemetrySeries int64 `json:"telemetry_series,omitempty"`
}

// PhaseResult is one phase's account of itself.
type PhaseResult struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// The machine side of the arrival rate: what the phase's own climb asked
	// for, and what the fleet delivered. Both are the harness's to state,
	// because it is the process that dials machines.
	//
	// They are separate fields because collapsing them hides the one case the
	// validity rule exists for — a generator that could not produce the load
	// reads exactly like a system that could not absorb it. Restating the
	// offered figure as the achieved one is that collapse wearing both names.
	OfferedAgentArrivalsPerSecond  float64 `json:"offered_agent_arrivals_per_second"`
	AchievedAgentArrivalsPerSecond float64 `json:"achieved_agent_arrivals_per_second"`

	// The technician side. The profile declares it and a browser-side generator
	// offers it, so this process carries what was asked for and leaves the
	// achieved half absent rather than inventing it — an absent figure is
	// readable, and a copied one is not.
	OfferedOperatorArrivalsPerSecond  float64  `json:"offered_operator_arrivals_per_second"`
	AchievedOperatorArrivalsPerSecond *float64 `json:"achieved_operator_arrivals_per_second,omitempty"`

	OfferedConnectedAgents  int `json:"offered_connected_agents"`
	AchievedConnectedAgents int `json:"achieved_connected_agents"`

	// Concurrent remote sessions, which are the technician's side of the wire
	// for the same reason the arrival rate above is. A profile declared five of
	// them and ran zero, and nothing said so; what was asked for now travels,
	// and the achieved half stays absent until a browser-side generator opens
	// them.
	OfferedSessions  int  `json:"offered_sessions"`
	AchievedSessions *int `json:"achieved_sessions,omitempty"`

	// TargetBusyPercent is what share of its declared processor allowance the
	// target used over this phase: its own processor counter, bracketed around
	// the phase and divided by the phase's clock and by what it was capped at.
	//
	// It is a pointer because a reading that could not be taken is absent. A
	// nought here would be a target that did no work at all — the healthiest
	// figure a server could report — so a run that never asked must not read as
	// the best run ever measured.
	TargetBusyPercent *float64 `json:"target_busy_percent,omitempty"`

	LatencyP50Ms float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95Ms float64 `json:"latency_p95_ms,omitempty"`
	LatencyP99Ms float64 `json:"latency_p99_ms,omitempty"`
	ErrorRate    float64 `json:"error_rate"`

	// ExpectedRejections is the system working: a refused write past a declared
	// limit, a duplicate connection closed, an admission deferred. Counting
	// those as faults makes a correctly enforced limit look like a defect and
	// buries the real ones, so they are held apart.
	ExpectedRejections int64 `json:"expected_rejections"`
	Faults             int64 `json:"faults"`
}

// AchievedFraction is how much of the offered arrival rate actually arrived.
//
// It reads the technician side when something measured it and the machine side
// otherwise, because those are the two ways a run offers arrivals and a phase
// carries whichever of them it drove. A phase that offered nothing counts as
// fully achieved: there was nothing to fall short of.
func (p PhaseResult) AchievedFraction() float64 {
	if p.AchievedOperatorArrivalsPerSecond != nil && p.OfferedOperatorArrivalsPerSecond > 0 {
		return *p.AchievedOperatorArrivalsPerSecond / p.OfferedOperatorArrivalsPerSecond
	}
	if p.OfferedAgentArrivalsPerSecond <= 0 {
		return 1
	}
	return p.AchievedAgentArrivalsPerSecond / p.OfferedAgentArrivalsPerSecond
}

// JourneyResult is one operator journey's account of itself, so a slow run can
// name which screen was slow rather than only that the API was.
type JourneyResult struct {
	Name      string  `json:"name"`
	Requests  int64   `json:"requests"`
	ErrorRate float64 `json:"error_rate"`
	// TargetBusyPercent is what share of its declared processor allowance the
	// target used over this phase: its own processor counter, bracketed around
	// the phase and divided by the phase's clock and by what it was capped at.
	//
	// It is a pointer because a reading that could not be taken is absent. A
	// nought here would be a target that did no work at all — the healthiest
	// figure a server could report — so a run that never asked must not read as
	// the best run ever measured.
	TargetBusyPercent *float64 `json:"target_busy_percent,omitempty"`

	LatencyP50Ms float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95Ms float64 `json:"latency_p95_ms,omitempty"`
}

// Observation is one timestamped sample of something the run watched rather
// than drove: server, database, telemetry and node series alike.
type Observation struct {
	At     time.Time         `json:"at"`
	Series string            `json:"series"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Headroom is what the generator had left. A run measured from a saturated
// generator is measuring the generator.
type Headroom struct {
	// Measured says the figures below came from somewhere. False is a run that
	// never looked, and a reading nobody took is not a reading of plenty: the
	// field was written as 100% free and 0% used on every run ever recorded,
	// which is what kept the saturation rule from ever firing.
	Measured bool `json:"measured"`

	// Scope is whose room this is: the generator's own allowance, or the box it
	// shares with the system under test. They are different statements, and the
	// rule that invalidates a run for a starved generator falls only on the
	// first — a busy box is what the throwaway venue is for.
	Scope string `json:"scope,omitempty"`

	CPUHeadroomPercent float64 `json:"cpu_headroom_percent"`
	MemoryUsedPercent  float64 `json:"memory_used_percent"`

	// CPURefusedPercent is the share of the run the generator spent runnable
	// and denied the processor, where the kernel keeps that account. A
	// generator kept waiting measured its own wait into every latency it
	// reported, whatever room it had left. Absent means the kernel counts no
	// refusals, which is not the same as none having happened.
	CPURefusedPercent *float64 `json:"cpu_refused_percent,omitempty"`
}

// CleanupProof is the run's account of what it left behind. It travels with the
// run rather than being checked once and assumed thereafter, because residue
// accumulates silently: an environment whose every user is load-test residue
// got there one uncleaned run at a time.
type CleanupProof struct {
	Verified      bool  `json:"verified"`
	OrphanUsers   int64 `json:"orphan_users"`
	OrphanDevices int64 `json:"orphan_devices"`
	OrphanTenants int64 `json:"orphan_tenants"`
	OrphanPods    int64 `json:"orphan_pods"`
}

// Clean reports whether the run left nothing behind.
func (c CleanupProof) Clean() bool {
	return c.OrphanUsers == 0 && c.OrphanDevices == 0 && c.OrphanTenants == 0 && c.OrphanPods == 0
}

// Bundle is one run's whole evidence.
type Bundle struct {
	SchemaVersion     int             `json:"schema_version"`
	Run               RunIdentity     `json:"run"`
	Target            Fingerprint     `json:"target"`
	Generator         Fingerprint     `json:"generator"`
	Fixture           FixtureCounts   `json:"fixture"`
	Phases            []PhaseResult   `json:"phases"`
	Journeys          []JourneyResult `json:"journeys"`
	Observations      []Observation   `json:"observations"`
	GeneratorHeadroom Headroom        `json:"generator_headroom"`
	Cleanup           CleanupProof    `json:"cleanup"`
	Verdict           Verdict         `json:"verdict"`
}

// Validate reports every reason this bundle could not be read as a run.
func (b *Bundle) Validate() error {
	var problems []error

	if b.SchemaVersion != bundleSchemaVersion {
		problems = append(problems, fmt.Errorf("schema_version %d is not %d", b.SchemaVersion, bundleSchemaVersion))
	}
	problems = append(problems, b.validateRun()...)
	problems = append(problems, validateFingerprint("target", b.Target)...)
	problems = append(problems, validateFingerprint("generator", b.Generator)...)
	problems = append(problems, b.validateFixture()...)
	problems = append(problems, b.validatePhases()...)

	if len(b.Observations) == 0 {
		problems = append(problems, errors.New("observations is empty — a run that watched nothing has only its own account of itself"))
	}
	problems = append(problems, b.validateCleanup()...)
	if b.Verdict.Result == "" {
		problems = append(problems, errors.New("verdict names no result"))
	}

	return errors.Join(problems...)
}

func (b *Bundle) validateRun() []error {
	var problems []error
	if b.Run.ID == "" {
		problems = append(problems, errors.New("run.id is empty"))
	}
	if b.Run.Commit == "" || b.Run.Commit == unknownCommit {
		problems = append(problems, fmt.Errorf(
			"run.commit is %q — a measurement with no source revision cannot be attributed to the code that produced it",
			b.Run.Commit))
	}
	if b.Run.ProfileName == "" {
		problems = append(problems, errors.New("run.profile_name is empty"))
	}
	if b.Run.ProfileVersion == 0 {
		problems = append(problems, errors.New("run.profile_version is unset"))
	}
	if b.Run.StartedAt.IsZero() {
		problems = append(problems, errors.New("run.started_at is unset"))
	}
	if b.Run.FinishedAt.Before(b.Run.StartedAt) {
		problems = append(problems, errors.New("run.finished_at is before run.started_at"))
	}
	return problems
}

// minPlausibleMemoryBytes is the floor below which a memory figure is a
// placeholder rather than a reading.
//
// It is a mebibyte, which no machine or container this repository runs anything
// on could be limited to and which every real reading clears by three orders of
// magnitude. The number it exists to refuse is one byte: both fingerprints were
// written as one processor and one byte of memory on every run, so four bundles
// from a sweep whose only subject was the processor count reported identical
// hardware and every latency figure beside them was uninterpretable.
const minPlausibleMemoryBytes = 1 << 20

func validateFingerprint(field string, f Fingerprint) []error {
	var problems []error
	if f.Kind == "" || f.Description == "" {
		problems = append(problems, fmt.Errorf("%s fingerprint names neither a kind nor a description", field))
	}
	if f.CPUs <= 0 {
		problems = append(problems, fmt.Errorf("%s fingerprint reports no processor count", field))
	}
	if f.MemoryBytes < minPlausibleMemoryBytes {
		problems = append(problems, fmt.Errorf(
			"%s fingerprint reports %d bytes of memory, which is below the %d-byte floor a real reading clears — a latency figure is a property of the pair, so a placeholder here makes every number beside it unreadable",
			field, f.MemoryBytes, minPlausibleMemoryBytes))
	}
	return problems
}

func (b *Bundle) validateFixture() []error {
	if b.Fixture.Size == "" {
		return []error{errors.New("fixture names no size — the same load against a different fleet is a different run")}
	}
	if b.Fixture.Devices <= 0 {
		return []error{errors.New("fixture reports no devices")}
	}
	return nil
}

func (b *Bundle) validatePhases() []error {
	if len(b.Phases) == 0 {
		return []error{errors.New("phases is empty — a run with no phase results measured nothing")}
	}

	// Whether this run could read the target at all. The two questions are
	// answered by the same page, so a run that read what the target was holding
	// could have read how hard it was working, and a phase that did not is a
	// reading somebody dropped rather than a venue that publishes none.
	readTheTarget := b.readTheTarget()

	var problems []error
	for i, phase := range b.Phases {
		if phase.Name == "" {
			problems = append(problems, fmt.Errorf("phase %d has no name", i))
		}
		if phase.FinishedAt.Before(phase.StartedAt) {
			problems = append(problems, fmt.Errorf("phase %q finished before it started", phase.Name))
		}
		if phase.ErrorRate < 0 || phase.ErrorRate > 1 {
			problems = append(problems, fmt.Errorf("phase %q error_rate %v is not a ratio", phase.Name, phase.ErrorRate))
		}
		if readTheTarget && phase.TargetBusyPercent == nil {
			problems = append(problems, fmt.Errorf(
				"phase %q carries no target busy-ness, on a run that read the target's own exposition — without it a target out of processor and one idle but slow are the same picture",
				phase.Name))
		}
	}
	return problems
}

// readTheTarget reports whether this run read the target's own account of
// itself, which is what the target series among the observations are.
//
// It is the document's own statement of the venue rather than a flag beside it:
// a bundle carrying those series was pointed at a page that answered, and the
// processor counter is on that same page.
func (b *Bundle) readTheTarget() bool {
	for _, observation := range b.Observations {
		if strings.HasPrefix(observation.Series, targetSeriesPrefix) {
			return true
		}
	}
	return false
}

// targetSeriesPrefix names the observations that come off the target's own
// exposition.
const targetSeriesPrefix = "target_"

func (b *Bundle) validateCleanup() []error {
	if !b.Cleanup.Verified {
		return []error{errors.New("cleanup was never verified — residue accumulates one unchecked run at a time")}
	}
	if !b.Cleanup.Clean() {
		return []error{fmt.Errorf("run left residue: %d users, %d devices, %d tenants, %d pods",
			b.Cleanup.OrphanUsers, b.Cleanup.OrphanDevices, b.Cleanup.OrphanTenants, b.Cleanup.OrphanPods)}
	}
	return nil
}

// WriteTo validates the bundle and writes it into dir, returning the path. An
// incomplete bundle never reaches disk: writing one is how it enters the trend.
func (b *Bundle) WriteTo(dir string) (string, error) {
	if err := b.Validate(); err != nil {
		return "", fmt.Errorf("bundle is incomplete, so the run has no evidence: %w", err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create bundle directory %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode bundle: %w", err)
	}
	path := filepath.Join(dir, bundleFileName)
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write bundle %s: %w", path, err)
	}
	return path, nil
}

// LoadBundle reads a bundle from disk without validating it, so a run that
// produced an unreadable bundle can still be inspected.
func LoadBundle(path string) (*Bundle, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read bundle %s: %w", path, err)
	}
	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("decode bundle %s: %w", path, err)
	}
	return &b, nil
}
