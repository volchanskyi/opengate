package main

import (
	"errors"
	"fmt"
)

// validateBreakingPoint refuses an answer that read no rung, since "nothing gave out" holds
// trivially for a ladder whose phases never arrived.
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

// validateLeakTrail refuses a trail that cannot have found anything: fewer than two readings,
// no interval, or no directory holding its profiles.
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

// validateCleanup refuses a cleanup nobody counted without saying why, and a count that found
// something left behind.
func (b *Bundle) validateCleanup() []error {
	if !b.Cleanup.Verified {
		if b.Cleanup.NotCounted != "" {
			return nil
		}
		return []error{errors.New("cleanup was never verified and the bundle does not say why — residue accumulates one unchecked run at a time")}
	}
	if !b.Cleanup.Clean() {
		return []error{fmt.Errorf("run left residue: %d users, %d devices, %d customers, %d sites",
			b.Cleanup.OrphanUsers, b.Cleanup.OrphanDevices, b.Cleanup.OrphanOrganizations, b.Cleanup.OrphanSites)}
	}
	return nil
}
