package main

import (
	"fmt"
)

// What a night is judged by, declared in the profile and nowhere else.
//
// These numbers used to live in two places at once. A script kept a set of
// absolute limits keyed by source/scenario/phase, and the profile kept its own,
// with different values for the same measurement — one enforced and one read by
// nothing. Whoever edited either did not change what they thought they were
// changing.
//
// The settlement below is that a measurement carries at most one limit that
// fails a night, per direction, and any number of marks that only report. Those
// are different statements: a floor under a collapse the fortnight comparison
// cannot see, and a target somebody has committed to and is watching before
// enforcing. Collapsing them into one number loses whichever is dropped.

// Gate is one rule the bundle is read against. It never reads live state and
// never changes the workload.
type Gate struct {
	// Series is a source/scenario/phase triple, matching the trend rows.
	Series string `yaml:"series"`
	Metric string `yaml:"metric"`
	// Max and Min are pointers so "no ceiling" is distinguishable from zero.
	Max *float64 `yaml:"max"`
	Min *float64 `yaml:"min"`
	// Blocking says whether a breach fails the run or is reported as a
	// finding. A newly tightened mark stays advisory until fresh runs show the
	// spread it is measured against has narrowed.
	//
	// A measurement carries at most one blocking gate and any number of
	// advisory marks. Those are different statements — a limit being enforced
	// and a target being watched — and a profile that collapsed them would lose
	// whichever it dropped. Two blocking gates on one measurement is the defect
	// this whole arrangement exists to close, reached inside one file.
	Blocking bool `yaml:"blocking"`
}

// Measurement is the series and metric a gate names, as one key. It is what
// makes "one measurement, one blocking gate" a thing that can be counted.
func (g Gate) Measurement() string { return g.Series + "|" + g.Metric }

// Ungated is a measurement deliberately left without a limit, and why.
//
// It exists because the absolute limits this profile took over had a catch-all:
// a series nobody listed was held to a default automatically. A profile has no
// catch-all, so moving the numbers across could delete protection while looking
// like consolidation. An entry here keeps a measurement accounted for without
// limiting it, and the reason is required for the same cause every exemption in
// this repository states one: an exemption nobody can review is never removed.
type Ungated struct {
	Series string `yaml:"series"`
	Metric string `yaml:"metric"`
	Reason string `yaml:"reason"`
}

// Measurement is the series and metric this declaration covers.
func (u Ungated) Measurement() string { return u.Series + "|" + u.Metric }

// DecidedMeasurements is every measurement this profile has ruled on, either
// way. A measurement absent from it is one nobody decided about, which reads
// identically to one deliberately left alone and is not the same thing.
func (p *Profile) DecidedMeasurements() map[string]bool {
	decided := make(map[string]bool, len(p.Gates)+len(p.Ungated))
	for _, gate := range p.Gates {
		decided[gate.Measurement()] = true
	}
	for _, ungated := range p.Ungated {
		decided[ungated.Measurement()] = true
	}
	return decided
}

func (p *Profile) validateGates() []error {
	var problems []error
	// A blocking gate is counted by measurement and by direction: a ceiling and
	// a floor on the same measurement answer different questions and are not a
	// duplicate of each other.
	blocking := map[string]int{}

	for i, gate := range p.Gates {
		if gate.Series == "" {
			problems = append(problems, fmt.Errorf("gate %d names no series", i))
		}
		if gate.Metric == "" {
			problems = append(problems, fmt.Errorf("gate %d names no metric", i))
		}
		if gate.Max == nil && gate.Min == nil {
			problems = append(problems, fmt.Errorf("gate %d (%s) declares neither max or min, so nothing can breach it",
				i, gate.Series))
		}
		if !gate.Blocking {
			continue
		}
		if gate.Max != nil {
			blocking[gate.Measurement()+"|max"]++
		}
		if gate.Min != nil {
			blocking[gate.Measurement()+"|min"]++
		}
	}

	for _, gate := range p.Gates {
		for _, direction := range []string{"max", "min"} {
			key := gate.Measurement() + "|" + direction
			if blocking[key] > 1 {
				problems = append(problems, fmt.Errorf(
					"%s %s carries more than one gate that fails the run: one measurement has one enforced limit per direction, and marks that only report sit beside it",
					gate.Series, gate.Metric))
				blocking[key] = 0
			}
		}
	}
	return problems
}

// validateUngated holds a deliberate absence to the same standard as a limit:
// it names a measurement, it says why, and it does not contradict a limit
// declared for the same measurement.
func (p *Profile) validateUngated() []error {
	var problems []error
	limited := make(map[string]bool, len(p.Gates))
	for _, gate := range p.Gates {
		limited[gate.Measurement()] = true
	}

	for i, ungated := range p.Ungated {
		if ungated.Series == "" {
			problems = append(problems, fmt.Errorf("ungated %d names no series", i))
		}
		if ungated.Metric == "" {
			problems = append(problems, fmt.Errorf("ungated %d names no metric", i))
		}
		if ungated.Reason == "" {
			problems = append(problems, fmt.Errorf(
				"ungated %d (%s %s) states no reason — an exemption nobody can review is one nobody ever removes",
				i, ungated.Series, ungated.Metric))
		}
		if limited[ungated.Measurement()] {
			problems = append(problems, fmt.Errorf(
				"%s %s is both limited and declared unlimited, and which of the two is meant is not for a reader to guess",
				ungated.Series, ungated.Metric))
		}
	}
	return problems
}
