package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// Every profile declares what the run must not push its machine past, and the
// check below is what reads those numbers.
//
// They are not about the verdict. Two different things are being protected, and
// a profile declares whichever of them its environment has:
//
//   - The processor ceiling protects a neighbour. Staging shares one node with
//     production, and a run that saturates it does not produce a bad measurement
//     — it makes the kubelet start choosing which pods to evict. A disposable
//     stack has no neighbour, and driving its processor is what the scaling
//     sweep is for, so such a profile declares no processor ceiling and this
//     leaves it alone.
//   - The memory and disk ceilings protect the measurement. Past them the node
//     has nowhere to put what the run produces, and the numbers describe a
//     machine out of room rather than the system under test. They hold wherever
//     the run is.
//
// A reading nobody took is not a reading of zero. An absent measurement fails
// the check rather than passing it, because a guard that treats "unknown" as
// "plenty of room" protects nothing at all.

// procMemInfo and procLoadAvg are the kernel's own accounts of memory and of the
// run queue. They are named here so the readers below take a fixed path.
const (
	procMemInfo = "/proc/meminfo"
	procLoadAvg = "/proc/loadavg"
)

// NodeReading is the machine the run shares, at one instant.
type NodeReading struct {
	// Measured says whether these figures came from anywhere. False means
	// nothing was read, which is a different thing from a machine at rest.
	Measured      bool
	CPUPercent    float64
	MemoryPercent float64
	// DiskPercent is how full the filesystem the database writes into is. Zero
	// when it was not read.
	DiskPercent float64
}

// SafetyReader takes one reading.
type SafetyReader func() NodeReading

// CheckSafety reports why a run must stop, or nil while it may continue.
func CheckSafety(limits Safety, reading NodeReading) error {
	if !reading.Measured {
		return errors.New("safety: the node was not measured, and an unmeasured node is not a node inside its limits")
	}

	var problems []error
	if limits.MaxNodeCPUPercent > 0 && reading.CPUPercent > limits.MaxNodeCPUPercent {
		problems = append(problems, fmt.Errorf(
			"the node's processor is %.0f%% committed against a limit of %.0f%% — production shares it",
			reading.CPUPercent, limits.MaxNodeCPUPercent))
	}
	if limits.MaxNodeMemoryPercent > 0 && reading.MemoryPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's memory is %.0f%% used against a limit of %.0f%% — past this the kubelet starts evicting",
			reading.MemoryPercent, limits.MaxNodeMemoryPercent))
	}
	// Disk is held to the memory ceiling rather than one of its own. Both are
	// the same statement — the node has nowhere left to put what the run
	// produces — and a second number to keep in step would be a second number to
	// forget. The message says which ceiling it is, so the reading is not
	// mistaken for a limit somebody declared for disks.
	if limits.MaxNodeMemoryPercent > 0 && reading.DiskPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's disk is %.0f%% full against the same %.0f%% ceiling as its memory — the database writes there",
			reading.DiskPercent, limits.MaxNodeMemoryPercent))
	}
	return errors.Join(problems...)
}

// RunPhasesWatched walks a profile and stops the moment the machine it shares
// goes past what the profile said it would accept.
func RunPhasesWatched(profile *Profile, fleet Fleet, clock Clock, read SafetyReader) ([]PhaseResult, error) {
	if profile == nil {
		return nil, errors.New("run phases: no profile — a run without one has no phases to walk")
	}
	if len(profile.Phases) == 0 {
		return nil, errors.New("run phases: the profile declares no phases")
	}

	results := make([]PhaseResult, 0, len(profile.Phases))
	from := 0
	for _, phase := range profile.Phases {
		if err := CheckSafety(profile.Safety, read()); err != nil {
			return results, fmt.Errorf("stopping before phase %q: %w", phase.Name, err)
		}
		result, err := runOnePhase(phase, from, fleet, clock)
		if err != nil {
			return nil, fmt.Errorf("phase %q: %w", phase.Name, err)
		}
		if err := CheckSafety(profile.Safety, read()); err != nil {
			return append(results, result), fmt.Errorf("stopping after phase %q: %w", phase.Name, err)
		}
		results = append(results, result)
		from = result.AchievedConnectedAgents
	}
	return results, nil
}

// LocalNodeReading is the machine this process is running on.
//
// It is the honest reading where the generator and the machine under test are
// the same box — the throwaway stack — and it is a reading of the runner rather
// than of a cluster node anywhere else. What it cannot see, it does not claim.
func LocalNodeReading() NodeReading {
	reading := NodeReading{Measured: true}

	if total, available, ok := readMemInfo(); ok {
		reading.MemoryPercent = float64(total-available) / float64(total) * 100
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(os.TempDir(), &stat); err == nil && stat.Blocks > 0 {
		used := stat.Blocks - stat.Bavail
		reading.DiskPercent = float64(used) / float64(stat.Blocks) * 100
	}

	// The run queue at this instant against the processor count. It is what one
	// look gives, reported as that rather than dressed up as utilisation, and it
	// describes the machine now rather than the minute before the look.
	if raw, err := os.ReadFile(procLoadAvg); err == nil {
		reading.CPUPercent = runQueuePercent(string(raw), runtime.NumCPU())
	}
	return reading
}

// readMemInfo reads total and available memory, in kilobytes.
func readMemInfo() (total, available int64, ok bool) {
	raw, err := os.ReadFile(procMemInfo)
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = value
		case "MemAvailable:":
			available = value
		}
	}
	return total, available, total > 0
}

// runQueuePercent turns one /proc/loadavg reading into how much of the machine
// is committed right now, as a percentage of its processors.
//
// It reads the fourth field — the tasks runnable at this instant — and not the
// one-minute average, which describes the minute before the reading. On a runner
// that minute is the one the job spent building images and a fleet, so the
// average reports the build as the run's own commitment and stops the run before
// its first phase.
//
// The reader itself is runnable while it reads, so it is subtracted: an
// otherwise idle machine is committed to nothing, not to one task.
//
// The figure is reported as it is rather than trimmed to a hundred. A node
// committed to four times what it has and one exactly full are different
// findings, and a ceiling comparison reads them the same way either way.
func runQueuePercent(raw string, processors int) float64 {
	runnable, ok := parseRunQueue(raw)
	if !ok || processors <= 0 {
		return 0
	}
	others := runnable - 1
	if others < 0 {
		others = 0
	}
	return float64(others) / float64(processors) * 100
}

// parseRunQueue reads the runnable-task count out of /proc/loadavg's fourth
// field, which has the form "runnable/total". A shape it cannot read reports no
// reading rather than a zero, because zero is a machine at rest and those are
// different answers.
func parseRunQueue(raw string) (int, bool) {
	fields := strings.Fields(raw)
	if len(fields) < 4 {
		return 0, false
	}
	runnable, _, found := strings.Cut(fields[3], "/")
	if !found {
		return 0, false
	}
	value, err := strconv.Atoi(runnable)
	if err != nil || value < 0 {
		return 0, false
	}
	return value, true
}
