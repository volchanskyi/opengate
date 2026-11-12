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
	// BlockingFromCPUs keeps a blocking gate a watched mark on a target given fewer processors,
	// a rung starved on purpose to draw a curve.
	BlockingFromCPUs *float64 `yaml:"blocking_from_cpus"`
	// Follows names another gate's measurement as series|metric; this gate's measurement reads
	// pass or over with that gate's outcome and states no figure of its own.
	Follows string `yaml:"follows"`
}

// Measurement is the series and metric a gate names, as one key.
func (g Gate) Measurement() string { return g.Series + "|" + g.Metric }

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
		problems = append(problems, p.validateGateShape(i, gate)...)
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

// validateGateShape requires a figure or a gate to follow, never both, and a processor share
// only on a failing gate.
func (p *Profile) validateGateShape(i int, gate Gate) []error {
	var problems []error
	switch {
	case gate.Follows != "" && (gate.Max != nil || gate.Min != nil):
		problems = append(problems, fmt.Errorf("gate %d (%s %s) follows another limit and states a figure of its own",
			i, gate.Series, gate.Metric))
	case gate.Follows != "" && !p.limits(gate.Follows):
		problems = append(problems, fmt.Errorf("gate %d (%s %s) follows %s, which no limit of this profile is",
			i, gate.Series, gate.Metric, gate.Follows))
	case gate.Follows == "" && gate.Max == nil && gate.Min == nil:
		problems = append(problems, fmt.Errorf("gate %d (%s) declares neither max or min, so nothing can breach it",
			i, gate.Series))
	}
	if gate.BlockingFromCPUs != nil && (*gate.BlockingFromCPUs <= 0 || !gate.Blocking) {
		problems = append(problems, fmt.Errorf(
			"gate %d (%s %s) sets blocking_from_cpus, which a failing gate takes as a positive share", i, gate.Series, gate.Metric))
	}
	return problems
}

// limits reports whether a gate of this profile with a figure names the measurement.
func (p *Profile) limits(measurement string) bool {
	for _, gate := range p.Gates {
		if gate.Measurement() == measurement && gate.Follows == "" {
			return true
		}
	}
	return false
}
