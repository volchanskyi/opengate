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
// checks below are what read those numbers.
//
// They are not about the verdict. Two different things are being protected, and
// a profile declares whichever of them its environment has:
//
//   - The processor ceiling protects a neighbour. Staging shares one node with
//     production, so a run is asked before it starts whether the neighbour has
//     left it room. A disposable stack has no neighbour, and driving its
//     processor is what the scaling sweep is for, so such a profile declares no
//     processor ceiling and this leaves it alone.
//   - The memory and disk ceilings protect the measurement. Past them the node
//     has nowhere to put what the run produces, and the numbers describe a
//     machine out of room rather than the system under test. They hold wherever
//     the run is, and for the whole of it.
//
// Which of those two a ceiling is decides when it can be read, and CheckRoomToStart
// carries the reasoning.
//
// A reading nobody took is not a reading of zero. An absent measurement fails
// the check rather than passing it, because a guard that treats "unknown" as
// "plenty of room" protects nothing at all.
//
// Which measure of a busy machine is honest depends on whose box it is, so the
// venue picks rather than the call site. See venueProcessorMeasure below.

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

// CheckRoomToStart reports why a run must not begin, or nil if it may.
//
// It is the only place the processor ceiling is read, and the reason is what
// processor time does under pressure. Memory and disk get used up: what the run
// puts there is gone until it gives it back, and past the ceiling the node has
// nowhere to put the next thing — so the run's own share is exactly what those
// ceilings ask about. Processor time is not used up, it is taken in turns. A
// node whose processors are over-committed serves everything more slowly, in
// proportion to what each pod was promised; it does not run out, and the kubelet
// does not evict anything for it.
//
// So a processor reading taken while the run is offering load is mostly a
// reading of the run's own work, and a ceiling on that stops a run for doing
// what it was asked to do. It did: staging's nightly was refused after its ramp
// phase with the node reading 108% against an 85% ceiling, on a node that reads
// between 9% and 39% when nothing is running on it.
//
// The question the ceiling exists to ask — is there room beside production
// tonight — has one moment when the answer is about the neighbour alone, and
// that is before the run has offered anything. What holds afterwards is not a
// reading at all but the kernel's own arithmetic: every pod on that node has a
// share it is guaranteed and a cap it cannot exceed, and the run's pods are
// capped at figures somebody chose.
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

// CheckRoomToContinue reports why a run already offering load must stop, or nil
// while it may carry on. It is the ceilings on room the node can actually run
// out of; see CheckRoomToStart for why the processor is not one of them.
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

// walkStartedAnnouncement is the line the run prints when it starts walking,
// followed by the second it started at. scripts/loadtest-quic-incluster.sh
// reads it, the way it already reads the fleet and estate announcements.
const walkStartedAnnouncement = "Walk started at"

// RunPhasesWatched walks a profile and stops the moment the machine it shares
// goes past what the profile said it would accept.
func RunPhasesWatched(profile *Profile, fleet Fleet, clock Clock, read SafetyReader, busy TargetBusy) ([]PhaseResult, error) {
	if profile == nil {
		return nil, errors.New("run phases: no profile — a run without one has no phases to walk")
	}
	if len(profile.Phases) == 0 {
		return nil, errors.New("run phases: the profile declares no phases")
	}

	// The walk says when it began, because something else has to join it.
	// The browser-side generators cannot start until the estate they read is
	// filed, which is after the arrivals — so without a time to join at, they
	// would start the shape again from its beginning and stay a phase behind
	// for the rest of the night, holding a steady window open past the drain
	// and publishing a percentile taken partly against a fleet that had already
	// left.
	fmt.Printf("%s %d\n", walkStartedAnnouncement, clock.Now().Unix())

	results := make([]PhaseResult, 0, len(profile.Phases))
	from := 0
	for i, phase := range profile.Phases {
		// The full check once, before anything has been offered, and the room
		// the node can run out of every time after that.
		check := CheckRoomToContinue
		if i == 0 {
			check = CheckRoomToStart
		}
		if err := check(profile.Safety, read()); err != nil {
			return results, fmt.Errorf("stopping before phase %q: %w", phase.Name, err)
		}
		result, err := runOnePhase(phase, from, fleet, clock, busy)
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

// processorMeasure turns one /proc/loadavg reading into how committed the
// machine is, as a percentage of its processors. A shape it could not read
// reports so rather than reporting nought, because nought is a machine at rest
// and an unasked question is not that.
//
// There are two of them and they answer different questions, which is the whole
// of the venue split below.
type processorMeasure func(raw string, processors int) (float64, bool)

// venueProcessorMeasure is the measure of processor commitment the venue calls
// for.
//
// A run that owns its box is read at the instant. The minute before such a
// reading is the job's own image build, so the average would report the build as
// the run's own commitment and stop the run before its first phase.
//
// A guest — a pod scheduled onto a node that carries production too — is read
// over the last minute, because that is what "is there room beside production"
// asks, and because the instant is a coin flip at that scale: on a two-processor
// node the run queue moves in fifty-point steps, so a node a third busy reads as
// a hundred or two hundred percent committed depending on which instant the look
// landed on.
//
// Which of the two this is comes from the same question that decides whose room
// the generator's own reading describes: whether the kernel gives this process a
// processor allowance of its own.
func venueProcessorMeasure(guest bool) processorMeasure {
	if guest {
		return loadAveragePercent
	}
	return runQueuePercent
}

// VenueNodeReading is the machine this run shares, read the way its venue calls
// for. It is what a profiled run walks against, so no call site has to remember
// which measure is honest where.
func VenueNodeReading() NodeReading {
	return readNode(venueProcessorMeasure(runHasItsOwnAllowance()))
}

// LocalNodeReading is the box this process owns, read at the instant.
//
// It is the honest reading where the generator and the machine under test are
// the same box — the throwaway stack — which is the one venue that reaches it.
// What it cannot see, it does not claim.
func LocalNodeReading() NodeReading {
	return readNode(runQueuePercent)
}

// readNode takes one reading of the machine this process is on, measuring its
// processors the way the caller asked for.
//
// A figure that did not come back leaves the whole reading unmeasured rather
// than nought. Nought is a machine with room to spare, so a reader that fills an
// unanswered question in with it hands every ceiling the one answer that always
// passes.
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

// runQueuePercent turns one /proc/loadavg reading into how much of the machine
// is committed right now, as a percentage of its processors.
//
// It reads the fourth field — the tasks runnable at this instant — and not the
// one-minute average, which describes the minute before the reading. It is the
// measure for a box the run owns, where that minute is the one the job spent
// building images and a fleet.
//
// The reader itself is runnable while it reads, so it is subtracted: an
// otherwise idle machine is committed to nothing, not to one task.
//
// The figure is reported as it is rather than trimmed to a hundred. A node
// committed to four times what it has and one exactly full are different
// findings, and a ceiling comparison reads them the same way either way.
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

// loadAveragePercent turns one /proc/loadavg reading into how much of the
// machine was committed over the last minute, as a percentage of its processors.
//
// It is the measure for a box this run is a guest on. The minute before the
// reading is production going about its business, which is exactly what a
// ceiling protecting a neighbour asks about — and unlike the instant it does not
// quantise: a node a third busy reads as a third busy rather than as whichever
// multiple of fifty percent the look happened to land on.
//
// Nothing is subtracted here. The average is over a minute this reader spent
// almost all of asleep, so it carries no meaningful weight of its own.
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
