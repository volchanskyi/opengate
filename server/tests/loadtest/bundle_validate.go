package main

import (
	"errors"
	"fmt"
	"strings"
)

// Whether a bundle can be read as a run at all.
//
// Every rule below refuses one way of producing a document that looks like
// evidence and is not: a section nobody filled in, a number that describes an
// intention rather than a reading, an absence that was never looked for. A
// bundle that fails any of them never reaches disk, because writing one is how
// it enters the trend — and a partial night absorbed as data lowers the window
// median, so the next genuinely slow night compares favourably against it and
// passes. One partial night quietly costs two.

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
	problems = append(problems, b.validateBreakingPoint()...)
	problems = append(problems, b.validateLeakTrail()...)
	if b.Verdict.Result == "" {
		problems = append(problems, errors.New("verdict names no result"))
	}

	return errors.Join(problems...)
}

// validateBreakingPoint refuses an answer that could not have been reached.
//
// "Nothing gave out" is an absence, and an absence is satisfied by the absence
// of the whole conversation: a ladder whose phases never arrived reports it just
// as readily as one that held all the way up. So an answer states how many rungs
// it read, and an answer that read none is not one.
func (b *Bundle) validateBreakingPoint() []error {
	if b.BreakingPoint == nil {
		return nil
	}
	var problems []error
	if b.BreakingPoint.RungsRead <= 0 {
		problems = append(problems, errors.New(
			"breaking_point read no rung — a ladder that looked at nothing did not find that nothing gave out"))
	}
	if b.BreakingPoint.GaveAt != "" && b.BreakingPoint.Reason == "" {
		problems = append(problems, fmt.Errorf(
			"breaking_point says %q gave out and does not say which reading decided it",
			b.BreakingPoint.GaveAt))
	}
	return problems
}

// validateLeakTrail refuses a trail that cannot have found anything.
//
// It is the rule the breaking point already carries, one field over: "nothing
// grew" is an absence, and an absence is satisfied by the absence of the whole
// conversation. A single reading has no difference in it, an interval of nought
// describes a watch that never ticked, and a trail naming nowhere to find its
// readings cannot be checked by anybody — each of the three reports a clean bill
// a leaking server would have produced just as readily.
func (b *Bundle) validateLeakTrail() []error {
	if b.Leak == nil {
		return nil
	}
	var problems []error
	if b.Leak.Snapshots < 2 {
		problems = append(problems, fmt.Errorf(
			"leak_trail carries %d reading(s) — a difference needs two, so this one found nothing because it looked once",
			b.Leak.Snapshots))
	}
	if b.Leak.IntervalSeconds <= 0 {
		problems = append(problems, errors.New(
			"leak_trail declares no interval, so its readings describe no stretch of the run"))
	}
	if b.Leak.Directory == "" {
		problems = append(problems, errors.New(
			"leak_trail names nowhere its profiles were kept, so nothing it reports can be checked"))
	}
	return problems
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
