package main

import (
	"errors"
	"fmt"
	"strings"
)

// Validate reports every reason this bundle could not be read as a run. A bundle that fails
// never reaches disk, because a partial night absorbed as data lowers the trend's window median.
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
	problems = append(problems, b.validateBreakingPoint()...)
	problems = append(problems, b.validateLeakTrail()...)
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

// minPlausibleMemoryBytes is one mebibyte, the floor below which a memory figure is a placeholder.
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

	// A run that read the target's census could also read its busy-ness, since one page
	// answers both.
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
		if phase.TargetBusyPercent != nil && phase.TargetBusyAbsent != "" {
			problems = append(problems, fmt.Errorf(
				"phase %q carries a target busy-ness and accounts for an absence of one — a reader has no way to tell which of the two is true",
				phase.Name))
		}
		if readTheTarget && phase.TargetBusyPercent == nil && phase.TargetBusyAbsent == "" {
			problems = append(problems, fmt.Errorf(
				"phase %q carries no target busy-ness and says nothing about why, on a run that read the target's own exposition — without it a target out of processor and one idle but slow are the same picture",
				phase.Name))
		}
	}
	return problems
}

// readTheTarget reports whether the observations include series read off the target's own
// exposition.
func (b *Bundle) readTheTarget() bool {
	for _, observation := range b.Observations {
		if strings.HasPrefix(observation.Series, targetSeriesPrefix) {
			return true
		}
	}
	return false
}

// targetSeriesPrefix names the observations that come off the target's own exposition.
const targetSeriesPrefix = "target_"
