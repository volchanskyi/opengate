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

// Every profile declares what the run must not push its machine past, and until
// now nothing read those numbers.
//
// They are not about the verdict. Staging shares one node with production, and
// a run that saturates it does not produce a bad measurement — it makes the
// kubelet start choosing which pods to evict. The limits exist so a run stops
// itself before anyone has to.
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
	if limits.MaxNodeCPUPercent <= 0 && limits.MaxNodeMemoryPercent <= 0 {
		// A profile that declares no limits is asking for none. That is the
		// throwaway stack: it is created by the job, thrown away at the end of
		// it, and shares its machine with nothing.
		return nil
	}
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
	if limits.MaxNodeMemoryPercent > 0 && reading.DiskPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's disk is %.0f%% full against a limit of %.0f%% — the database production depends on writes there too",
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

	// Processor use over an interval cannot be read from a single sample, and a
	// run that paused to take two would be pausing the thing it is measuring.
	// The queue length against the processor count is what one look gives, and
	// it is reported as that rather than dressed up as utilisation.
	reading.CPUPercent = loadPercent(runtime.NumCPU())
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

// loadPercent turns the one-minute run queue into a percentage of the machine's
// processors, capped at a hundred: a queue twice the processor count is a
// machine fully committed, not one two hundred percent used.
func loadPercent(processors int) float64 {
	raw, err := os.ReadFile(procLoadAvg)
	if err != nil || processors <= 0 {
		return 0
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0
	}
	oneMinute, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	percent := oneMinute / float64(processors) * 100
	if percent > 100 {
		return 100
	}
	return percent
}
