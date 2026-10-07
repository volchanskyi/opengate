package main

import (
	"fmt"
)

// Gate is one rule the bundle is read against.
type Gate struct {
	// Series is a source/scenario/phase triple matching the trend rows.
	Series string `yaml:"series"`
	Metric string `yaml:"metric"`
	// Max and Min are pointers so an absent limit differs from zero.
	Max *float64 `yaml:"max"`
	Min *float64 `yaml:"min"`
	// Blocking makes a breach fail the run; otherwise it is reported as a finding.
	// A measurement carries at most one blocking gate per direction and any number of marks.
	Blocking bool `yaml:"blocking"`
}

// Measurement is the series and metric a gate names, as one key.
func (g Gate) Measurement() string { return g.Series + "|" + g.Metric }

// Ungated is a measurement deliberately left without a limit, with the reason it is.
type Ungated struct {
	Series string `yaml:"series"`
	Metric string `yaml:"metric"`
	Reason string `yaml:"reason"`
}

// Measurement is the series and metric this declaration covers.
func (u Ungated) Measurement() string { return u.Series + "|" + u.Metric }

// DecidedMeasurements is every measurement this profile limits or declares ungated.
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
	// A blocking gate is counted by measurement and direction, so a ceiling and a floor coexist.
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

// validateUngated requires a measurement and a reason, and rejects one that also carries a limit.
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
