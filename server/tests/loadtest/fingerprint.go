package main

import (
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// ReadGeneratorShape is the generator as a fingerprint: its cgroup allowance where there is
// one, and the machine it runs on otherwise; the disk figure is the free space.
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
	if files, ok := openOwnCgroup(); ok {
		shape = withinAllowance(shape, files)
		files.close()
	}
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

	// Available blocks exclude those reserved for root, and the product is bounded so a huge
	// filesystem cannot overflow into a negative size.
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

// ParseFingerprintFlags turns the processor and memory figures a caller passed in into a
// fingerprint, leaving a figure zero where none was given.
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
