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

// procMemInfo and procLoadAvg are the kernel's own accounts of memory and of the run queue.
const (
	procMemInfo = "/proc/meminfo"
	procLoadAvg = "/proc/loadavg"
)

// NodeReading is the machine the run shares, at one instant.
type NodeReading struct {
	// Measured reports whether the figures were read from the node.
	Measured      bool
	CPUPercent    float64
	MemoryPercent float64
	// DiskPercent is how full the database's filesystem is; zero when unread.
	DiskPercent float64
}

// SafetyReader takes one reading.
type SafetyReader func() NodeReading

// CheckRoomToStart reports why a run may not begin, or nil if it may. It alone reads the
// processor ceiling, since a reading under offered load measures the run's own work.
func CheckRoomToStart(limits Safety, reading NodeReading) error {
	if !reading.Measured {
		return errors.New("safety: the node was not measured, and an unmeasured node is not a node inside its limits")
	}

	var problems []error
	if limits.MaxNodeCPUPercent > 0 && reading.CPUPercent > limits.MaxNodeCPUPercent {
		problems = append(problems, fmt.Errorf(
			"the node's processor is %.0f%% committed against a limit of %.0f%% — production shares it",
			reading.CPUPercent, limits.MaxNodeCPUPercent))
	}
	return errors.Join(append(problems, CheckRoomToContinue(limits, reading))...)
}

// CheckRoomToContinue reports why a run already offering load must stop, or nil while it may
// carry on. It covers the ceilings on room the node can run out of.
func CheckRoomToContinue(limits Safety, reading NodeReading) error {
	if !reading.Measured {
		return errors.New("safety: the node was not measured, and an unmeasured node is not a node inside its limits")
	}

	var problems []error
	if limits.MaxNodeMemoryPercent > 0 && reading.MemoryPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's memory is %.0f%% used against a limit of %.0f%% — past this the kubelet starts evicting",
			reading.MemoryPercent, limits.MaxNodeMemoryPercent))
	}
	// Disk shares the memory ceiling, as both mean the node has no room left for the run's output.
	if limits.MaxNodeMemoryPercent > 0 && reading.DiskPercent > limits.MaxNodeMemoryPercent {
		problems = append(problems, fmt.Errorf(
			"the node's disk is %.0f%% full against the same %.0f%% ceiling as its memory — the database writes there",
			reading.DiskPercent, limits.MaxNodeMemoryPercent))
	}
	return errors.Join(problems...)
}

// walkStartedAnnouncement is printed when the walk starts, followed by the start second;
// scripts/loadtest-quic-incluster.sh reads it.
const walkStartedAnnouncement = "Walk started at"

// RunPhasesWatched walks a profile and stops the moment the machine it shares
// goes past what the profile said it would accept.
func RunPhasesWatched(profile *Profile, fleet Fleet, clock Clock, read SafetyReader, readings PhaseReadings) ([]PhaseResult, error) {
	if profile == nil {
		return nil, errors.New("run phases: no profile — a run without one has no phases to walk")
	}
	if len(profile.Phases) == 0 {
		return nil, errors.New("run phases: the profile declares no phases")
	}

	// The start time lets the browser-side generators, which begin once the estate is filed,
	// join the shape at the right phase.
	fmt.Printf("%s %d\n", walkStartedAnnouncement, clock.Now().Unix())

	results := make([]PhaseResult, 0, len(profile.Phases))
	from := 0
	for i, phase := range profile.Phases {
		// The full check runs once before anything is offered; later checks cover room only.
		check := CheckRoomToContinue
		if i == 0 {
			check = CheckRoomToStart
		}
		if err := check(profile.Safety, read()); err != nil {
			return results, fmt.Errorf("stopping before phase %q: %w", phase.Name, err)
		}
		result, err := runOnePhase(phase, from, fleet, clock, readings)
		if err != nil {
			return nil, fmt.Errorf("phase %q: %w", phase.Name, err)
		}
		if err := CheckRoomToContinue(profile.Safety, read()); err != nil {
			return append(results, result), fmt.Errorf("stopping after phase %q: %w", phase.Name, err)
		}
		results = append(results, result)
		from = result.AchievedConnectedAgents
	}
	return results, nil
}

// processorMeasure turns one /proc/loadavg reading into the percentage of processors committed;
// false marks a shape it could not read.
type processorMeasure func(raw string, processors int) (float64, bool)

// venueProcessorMeasure reads the instant run queue on a box the run owns, and the one-minute
// average for a guest, whose instant reading moves in 50-point steps on two processors.
func venueProcessorMeasure(guest bool) processorMeasure {
	if guest {
		return loadAveragePercent
	}
	return runQueuePercent
}

// VenueNodeReading is the machine this run shares, read the way its venue calls for.
func VenueNodeReading() NodeReading {
	return readNode(venueProcessorMeasure(runHasItsOwnAllowance()))
}

// LocalNodeReading is the box this process owns, read at the instant, for a generator that
// shares the target's box.
func LocalNodeReading() NodeReading {
	return readNode(runQueuePercent)
}

// readNode takes one reading of this machine; a figure that did not come back leaves the whole
// reading unmeasured, since zero would pass every ceiling.
func readNode(measure processorMeasure) NodeReading {
	var reading NodeReading

	total, available, memoryRead := readMemInfo()
	if memoryRead {
		reading.MemoryPercent = float64(total-available) / float64(total) * 100
	}

	var stat syscall.Statfs_t
	diskRead := syscall.Statfs(os.TempDir(), &stat) == nil && stat.Blocks > 0
	if diskRead {
		used := stat.Blocks - stat.Bavail
		reading.DiskPercent = float64(used) / float64(stat.Blocks) * 100
	}

	var processorsRead bool
	if raw, err := os.ReadFile(procLoadAvg); err == nil {
		reading.CPUPercent, processorsRead = measure(string(raw), runtime.NumCPU())
	}

	reading.Measured = memoryRead && diskRead && processorsRead
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

// runQueuePercent reports instant commitment: runnable tasks, less the reader itself,
// as an uncapped percentage of processors.
func runQueuePercent(raw string, processors int) (float64, bool) {
	runnable, ok := parseRunQueue(raw)
	if !ok || processors <= 0 {
		return 0, false
	}
	others := runnable - 1
	if others < 0 {
		others = 0
	}
	return float64(others) / float64(processors) * 100, true
}

// loadAveragePercent reports the one-minute load average as a percentage of processors, with
// nothing subtracted since the reader slept through the minute.
func loadAveragePercent(raw string, processors int) (float64, bool) {
	if processors <= 0 {
		return 0, false
	}
	fields := strings.Fields(raw)
	if len(fields) < 1 {
		return 0, false
	}
	average, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || average < 0 {
		return 0, false
	}
	return average / float64(processors) * 100, true
}

// parseRunQueue reads the runnable count from the "runnable/total" fourth field;
// an unreadable shape reports no reading.
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
