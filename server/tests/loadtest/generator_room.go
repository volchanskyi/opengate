package main

import "time"

// GeneratorRoom reads the room the generator had over one phase's own window.
type GeneratorRoom struct {
	// ReadCPU reads the generator's own processor accounts and reports whether they were readable.
	ReadCPU func() (cgroupCPU, bool)
	// Allowance is the processors the generator is permitted, which its room is a share of.
	Allowance float64
}

// Bracket takes the opening reading and returns what closes it over the phase's window;
// a generator with no allowance of its own reports no room.
func (g GeneratorRoom) Bracket() func(window time.Duration) (headroom, refused *float64) {
	if g.ReadCPU == nil || g.Allowance <= 0 {
		return func(time.Duration) (*float64, *float64) { return nil, nil }
	}
	before, opened := g.ReadCPU()
	return func(window time.Duration) (*float64, *float64) {
		after, closed := g.ReadCPU()
		if !opened || !closed || window <= 0 {
			return nil, nil
		}
		headroom, refused := roomOver(before, after, window, g.Allowance)
		return &headroom, refused
	}
}

// NewGeneratorRoom reads this process's own cgroup allowance, and is empty without one.
func NewGeneratorRoom() GeneratorRoom {
	files, ok := openOwnCgroup()
	if !ok {
		return GeneratorRoom{}
	}
	defer files.close()
	allowance, granted := readGeneratorAllowance(files)
	if !granted {
		return GeneratorRoom{}
	}
	return GeneratorRoom{
		ReadCPU: func() (cgroupCPU, bool) {
			own, ok := openOwnCgroup()
			if !ok {
				return cgroupCPU{}, false
			}
			defer own.close()
			return readCgroupCPU(own)
		},
		Allowance: allowance.Processors,
	}
}

// withinAllowance describes a generator by the allowance its cgroup grants, where it grants one.
func withinAllowance(shape Fingerprint, files cgroupFiles) Fingerprint {
	allowance, granted := readGeneratorAllowance(files)
	if !granted {
		return shape
	}
	shape.CPUs = allowance.Processors
	if allowance.MemoryBytes > 0 {
		shape.MemoryBytes = allowance.MemoryBytes
	}
	return shape
}
