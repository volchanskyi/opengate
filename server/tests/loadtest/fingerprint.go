package main

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// The shape of each side of a measurement.
//
// A latency figure belongs to a pair, so a bundle that names neither side
// describes nothing that can be compared against anything. One side this
// process can read off the machine it is on; the other is known only to
// whoever started it.

// ReadGeneratorShape is the machine this process runs on, as a fingerprint.
//
// The disk figure is what is free rather than how big the partition is. A
// runner's root filesystem is 144 GiB and about 14 GB of it is room a job can
// use, the rest being the preinstalled toolchain — so the one measurement ever
// taken to answer "does this fixture fit" reported a number ten times the one
// that binds.
func ReadGeneratorShape(description string) Fingerprint {
	shape := Fingerprint{
		Kind:        "generator",
		Description: description,
		CPUs:        float64(runtime.NumCPU()),
		Arch:        runtime.GOARCH,
	}
	if total, _, ok := readMemInfo(); ok {
		shape.MemoryBytes = total * 1024
	}
	shape.DiskBytes = freeDiskBytes(os.TempDir())
	return shape
}

// freeDiskBytes is the room left on the filesystem holding path, or zero when
// it could not be read.
func freeDiskBytes(path string) int64 {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0
	}
	if stat.Bsize <= 0 {
		return 0
	}

	// Available to an unprivileged writer, which is what a run is. The blocks a
	// filesystem reserves for root are not room this process has.
	//
	// The product is bounded before it is taken: a filesystem past the signed
	// range would otherwise report a negative amount of room, which reads as a
	// full disk — the opposite of the finding.
	blocks := safeInt64(stat.Bavail)
	if blocks > math.MaxInt64/stat.Bsize {
		return math.MaxInt64
	}
	return blocks * stat.Bsize
}

// safeInt64 narrows a kernel-supplied count the caller has not bounded,
// clamping anything past the signed range so the conversion cannot overflow.
func safeInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// ParseFingerprintFlags turns the processor and memory figures a caller was
// given into a fingerprint, reporting nothing where it was told nothing.
//
// The system under test's own limits are known to whoever started it — they are
// the sweep's matrix value — and known to nothing inside this process, which
// sees a network address. So they are passed in, and a run given neither says
// so rather than inventing one processor and one byte.
func ParseFingerprintFlags(kind, description, cpus, memory string) Fingerprint {
	shape := Fingerprint{Kind: kind, Description: description}
	if parsed, err := strconv.ParseFloat(strings.TrimSpace(cpus), 64); err == nil {
		shape.CPUs = parsed
	}
	if parsed, err := strconv.ParseInt(strings.TrimSpace(memory), 10, 64); err == nil {
		shape.MemoryBytes = parsed
	}
	return shape
}
