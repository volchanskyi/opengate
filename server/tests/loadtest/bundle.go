package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// bundleSchemaVersion is the shape of the bundle document; it travels inside the document so a
// trend never silently spans two meanings of a field.
const bundleSchemaVersion = 12

// bundleFileName is what a bundle directory holds.
const bundleFileName = "bundle.json"

// RunIdentity says which run this is and what produced it.
type RunIdentity struct {
	ID     string `json:"id"`
	Commit string `json:"commit"`
	// ProfileName and ProfileVersion together say what was asked for, because a profile is
	// edited in place.
	ProfileName    string      `json:"profile_name"`
	ProfileVersion int         `json:"profile_version"`
	Family         Family      `json:"family"`
	Environment    Environment `json:"environment"`
	StartedAt      time.Time   `json:"started_at"`
	FinishedAt     time.Time   `json:"finished_at"`
}

// Fingerprint describes one side of the measurement; a latency figure is a property of the
// generator and target pair.
type Fingerprint struct {
	Kind        string `json:"kind"`
	Description string `json:"description"`
	// CPUs is fractional because a container's share of a machine is.
	CPUs        float64 `json:"cpus"`
	MemoryBytes int64   `json:"memory_bytes"`
	// DiskBytes is the free room on this side; zero means it was not measured.
	DiskBytes int64  `json:"disk_bytes,omitempty"`
	Arch      string `json:"arch,omitempty"`
}

// FixtureCounts is how much data was already there, plus the measured on-disk weight where the
// run took one.
type FixtureCounts struct {
	Size      FixtureSize `json:"size"`
	Tenants   int         `json:"tenants"`
	Customers int         `json:"customers"`
	Sites     int         `json:"sites"`
	Users     int         `json:"users"`
	// Devices is the machines that enrolled; PlannedDevices is what the plan asked for.
	Devices        int `json:"devices"`
	PlannedDevices int `json:"planned_devices,omitempty"`
	// DatabaseBytes and TelemetrySeries are filled by a run that weighed the fixture; zero means
	// it was not measured.
	DatabaseBytes   int64 `json:"database_bytes,omitempty"`
	TelemetrySeries int64 `json:"telemetry_series,omitempty"`
	// FiledDevices is how many machines the run filed under a customer and one of its sites.
	// It is a pointer because a run with no fixture filed for nobody, which differs from nought.
	FiledDevices *int `json:"filed_devices,omitempty"`
	// FilingRefusals is how many filings the server refused.
	FilingRefusals *int `json:"filing_refusals,omitempty"`
}

// PhaseResult is one phase's account of itself.
type PhaseResult struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// The harness dials the machines, so it states both the offered and the achieved rate.
	OfferedAgentArrivalsPerSecond  float64 `json:"offered_agent_arrivals_per_second"`
	AchievedAgentArrivalsPerSecond float64 `json:"achieved_agent_arrivals_per_second"`

	// The achieved technician rate stays absent unless a browser-side generator measured it.
	OfferedOperatorArrivalsPerSecond  float64  `json:"offered_operator_arrivals_per_second"`
	AchievedOperatorArrivalsPerSecond *float64 `json:"achieved_operator_arrivals_per_second,omitempty"`

	OfferedConnectedAgents  int `json:"offered_connected_agents"`
	AchievedConnectedAgents int `json:"achieved_connected_agents"`

	// The achieved session count stays absent until a browser-side generator opens sessions.
	OfferedSessions  int  `json:"offered_sessions"`
	AchievedSessions *int `json:"achieved_sessions,omitempty"`

	// TargetBusyPercent is the share of its processor allowance the target used over this phase;
	// a pointer because an unread value is absent, where nought would mean no work.
	TargetBusyPercent *float64 `json:"target_busy_percent,omitempty"`

	// TargetBusyAbsent is why the reading above is absent, where the run can say; it is empty
	// while the reading is present.
	TargetBusyAbsent string `json:"target_busy_absent,omitempty"`

	// TargetConnectedAgents is the fleet the target held when the phase closed, read beside
	// TargetGoroutines; both are pointers because nought would mean the target held nobody.
	TargetConnectedAgents *int     `json:"target_connected_agents,omitempty"`
	TargetGoroutines      *float64 `json:"target_goroutines,omitempty"`

	// TargetResidentBytes is the target's memory at the same reading.
	TargetResidentBytes *float64 `json:"target_resident_bytes,omitempty"`

	// TargetCensusAbsent is why the census reading is absent, where the run can say.
	TargetCensusAbsent string `json:"target_census_absent,omitempty"`

	// TargetCensusWaitedMs is how long the run held still while the target admitted machines it
	// had already accepted; nought is the healthy answer.
	TargetCensusWaitedMs float64 `json:"target_census_waited_ms,omitempty"`

	// The two terms bound how far the run's count and the target's count may differ: the
	// target's answer describes an instant between the counts before and after the question.
	ConnectedAgentsBeforeCensus int   `json:"connected_agents_before_census,omitempty"`
	DeparturesDuringCensus      int64 `json:"departures_during_census,omitempty"`

	LatencyP50Ms float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95Ms float64 `json:"latency_p95_ms,omitempty"`
	LatencyP99Ms float64 `json:"latency_p99_ms,omitempty"`
	ErrorRate    float64 `json:"error_rate"`

	// GeneratorCPUHeadroomPercent is the unused share of the generator's processor allowance and
	// GeneratorCPURefusedPercent the share of the phase it spent runnable but refused the processor.
	GeneratorCPUHeadroomPercent *float64 `json:"generator_cpu_headroom_percent,omitempty"`
	GeneratorCPURefusedPercent  *float64 `json:"generator_cpu_refused_percent,omitempty"`

	// GeneratorUDPReceiveErrors and TargetUDPReceiveErrors are the datagrams each end's kernel
	// dropped over this phase on a full receive buffer; absent where the counters are unreadable.
	GeneratorUDPReceiveErrors *int64 `json:"generator_udp_receive_errors,omitempty"`
	TargetUDPReceiveErrors    *int64 `json:"target_udp_receive_errors,omitempty"`

	// ExpectedRejections is the system enforcing a declared limit, held apart from Faults.
	ExpectedRejections int64 `json:"expected_rejections"`
	Faults             int64 `json:"faults"`
}

// OfferedAgentArrivals reports whether this phase reached for machines of its own; a winding-down
// phase offers none, and its outcomes belong to machines an earlier phase reached for.
func (p PhaseResult) OfferedAgentArrivals() bool {
	return p.OfferedAgentArrivalsPerSecond > 0
}

// AchievedFraction is the share of the offered arrival rate that arrived, read from the technician
// side when measured and the machine side otherwise; a phase that offered nothing achieved all.
func (p PhaseResult) AchievedFraction() float64 {
	if p.AchievedOperatorArrivalsPerSecond != nil && p.OfferedOperatorArrivalsPerSecond > 0 {
		return *p.AchievedOperatorArrivalsPerSecond / p.OfferedOperatorArrivalsPerSecond
	}
	if p.OfferedAgentArrivalsPerSecond <= 0 {
		return 1
	}
	return p.AchievedAgentArrivalsPerSecond / p.OfferedAgentArrivalsPerSecond
}

// JourneyResult is one operator journey's account of itself, so a slow run can name the slow
// screen.
type JourneyResult struct {
	Name      string  `json:"name"`
	Requests  int64   `json:"requests"`
	ErrorRate float64 `json:"error_rate"`
	// TargetBusyPercent is a pointer because an unread value is absent, where nought would mean
	// the target did no work.
	TargetBusyPercent *float64 `json:"target_busy_percent,omitempty"`

	LatencyP50Ms float64 `json:"latency_p50_ms,omitempty"`
	LatencyP95Ms float64 `json:"latency_p95_ms,omitempty"`
}

// Observation is one timestamped sample of something the run watched.
type Observation struct {
	At     time.Time         `json:"at"`
	Series string            `json:"series"`
	Value  float64           `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

// Headroom is what the generator had left.
type Headroom struct {
	// Measured is false for a run that never looked, whose figures carry no information.
	Measured bool `json:"measured"`

	// Scope is whose room this is: the generator's own allowance, or the box it shares with the
	// system under test. Only the first invalidates a run for a starved generator.
	Scope string `json:"scope,omitempty"`

	CPUHeadroomPercent float64 `json:"cpu_headroom_percent"`
	MemoryUsedPercent  float64 `json:"memory_used_percent"`

	// CPURefusedPercent is the share of the run the generator spent runnable and denied the
	// processor. Absent where the kernel counts no refusals.
	CPURefusedPercent *float64 `json:"cpu_refused_percent,omitempty"`
}

// CleanupProof is the account of what the run left behind, folded in by
// scripts/loadtest-bundle-merge.sh from the four kinds scripts/loadtest-cleanup.sh removes.
type CleanupProof struct {
	Verified bool `json:"verified"`
	// NotCounted is why no count is here, where the run can say.
	NotCounted          string `json:"not_counted,omitempty"`
	OrphanUsers         int64  `json:"orphan_users"`
	OrphanDevices       int64  `json:"orphan_devices"`
	OrphanOrganizations int64  `json:"orphan_organizations"`
	OrphanSites         int64  `json:"orphan_sites"`
}

// Clean reports whether the count found nothing left behind.
func (c CleanupProof) Clean() bool {
	return c.OrphanUsers == 0 && c.OrphanDevices == 0 && c.OrphanOrganizations == 0 && c.OrphanSites == 0
}

// RefusalCount is what the run's browser-side generators asked for and how much of it the server
// turned away at the door.
type RefusalCount struct {
	Requests int64 `json:"requests"`
	Refused  int64 `json:"refused"`
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
	// BreakingPoint is where the ladder broke, for a profile that declared what breaking means.
	BreakingPoint *BreakingPoint `json:"breaking_point,omitempty"`
	// Leak is what grew inside the target across the run. Absent for a run not asked to watch,
	// which differs from a run that watched and found nothing.
	Leak *LeakTrail `json:"leak_trail,omitempty"`
	// Refusals is how much of what the run asked for the server turned away. Absent where no
	// browser-side generator ran, because nought of nought would read as the cleanest night.
	Refusals *RefusalCount `json:"refusals,omitempty"`
}

// WriteTo validates the bundle and writes it into dir, returning the path. An incomplete bundle
// never reaches disk.
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

// LoadBundle reads a bundle from disk without validating it, so an unreadable bundle can still
// be inspected.
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
