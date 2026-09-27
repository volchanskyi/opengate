package main

import "time"

// The generator's room over one phase, and the generator described by the room
// it is given.
//
// The run's own reading covers the whole walk; a phase reads its own window
// from the same cgroup accounts, and the fingerprint names the allowance rather
// than the box around it.

// GeneratorRoom is how a phase reads the room the generator had while that
// phase ran. The run's own reading covers the whole walk, which on a ladder is
// mostly its quiet bottom rungs; a phase reads the rung it offered.
type GeneratorRoom struct {
	// ReadCPU is the generator's own processor accounts, and whether they
	// could be read.
	ReadCPU func() (cgroupCPU, bool)
	// Allowance is the processors the generator is permitted, which is what
	// its room is a share of.
	Allowance float64
}

// Bracket takes the opening reading and returns what closes it over the
// phase's own window. A generator with no allowance of its own has no room to
// divide, and every phase reports none.
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

// NewGeneratorRoom reads this process's own cgroup, where the kernel gives it
// an allowance, and nothing otherwise.
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

// withinAllowance describes a generator by the allowance its cgroup grants,
// where it grants one. The box around a generator held to one processor is
// somebody else's room, and a fingerprint of the box describes the wrong side
// of the measurement.
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
