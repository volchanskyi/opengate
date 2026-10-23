package main

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procMemInfo, procLoadAvg and procStat are the kernel's own accounts of memory, of the run
// queue and of the time the processors spent working.
const (
	procMemInfo = "/proc/meminfo"
	procLoadAvg = "/proc/loadavg"
	procStat    = "/proc/stat"
)

// firstBusyWindow is how long a guest's first processor reading spans.
const firstBusyWindow = 10 * time.Second

// minBusyWindow is the shortest span of a guest's later readings; the kernel counts processor
// time in ticks, so two samples closer together can see no time pass.
const minBusyWindow = time.Second

// processorReading measures how committed this machine's processors are; false marks a reading
// it could not take.
type processorReading func() (float64, bool)

// readProcStat reads the kernel's processor time account.
func readProcStat() (string, error) {
	raw, err := os.ReadFile(procStat)
	return string(raw), err
}

// readProcLoadAvg reads the kernel's run-queue account.
func readProcLoadAvg() (string, error) {
	raw, err := os.ReadFile(procLoadAvg)
	return string(raw), err
}

// runQueueReading reads the instant run queue, for a box the run owns.
func runQueueReading(readLoadAvg func() (string, error)) processorReading {
	return func() (float64, bool) {
		raw, err := readLoadAvg()
		if err != nil {
			return 0, false
		}
		return runQueuePercent(raw, runtime.NumCPU())
	}
}

// venueProcessorReading reads a box the run owns by its run queue at the instant, and a guest by
// the share of time the host's processors spent working, which a queue of waiting tasks misstates.
func venueProcessorReading(guest bool, clock Clock, readStat, readLoadAvg func() (string, error)) processorReading {
	if guest {
		return busyShareReading(clock, readStat)
	}
	return runQueueReading(readLoadAvg)
}

// NewVenueReader reads the machine this run shares the way its venue calls for.
func NewVenueReader(clock Clock) SafetyReader {
	measure := venueProcessorReading(runHasItsOwnAllowance(), clock, readProcStat,
		readProcLoadAvg)
	return func() NodeReading { return readNode(measure) }
}

// LocalNodeReading is the box this process owns, read at the instant, for a generator that
// shares the target's box.
func LocalNodeReading() NodeReading {
	return readNode(runQueueReading(readProcLoadAvg))
}

// readNode takes one reading of this machine; a figure that did not come back leaves the whole
// reading unmeasured, since zero would pass every ceiling.
func readNode(measure processorReading) NodeReading {
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
	reading.CPUPercent, processorsRead = measure()

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

// cpuTimes is the host's processor time from the first line of /proc/stat, in clock ticks:
// working, and idle or waiting on a disk. Guest time is already inside user and nice.
type cpuTimes struct {
	busy, idle uint64
}

// parseCPUTimes reads the aggregate `cpu` line; time the hypervisor took back counts as busy,
// since the run cannot have it.
func parseCPUTimes(raw string) (cpuTimes, bool) {
	line, _, _ := strings.Cut(raw, "\n")
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] != "cpu" {
		return cpuTimes{}, false
	}
	var ticks [8]uint64
	for i := range ticks {
		value, err := strconv.ParseUint(fields[i+1], 10, 64)
		if err != nil {
			return cpuTimes{}, false
		}
		ticks[i] = value
	}
	user, nice, system, idle, iowait, irq, softirq, steal := ticks[0], ticks[1], ticks[2], ticks[3],
		ticks[4], ticks[5], ticks[6], ticks[7]
	return cpuTimes{busy: user + nice + system + irq + softirq + steal, idle: idle + iowait}, true
}

// busyPercent is the share of the time between two readings the processors spent working; false
// marks no time passed, or counters that ran backwards.
func busyPercent(before, after cpuTimes) (float64, bool) {
	if after.busy < before.busy || after.idle < before.idle {
		return 0, false
	}
	busy, idle := after.busy-before.busy, after.idle-before.idle
	if busy+idle == 0 {
		return 0, false
	}
	return float64(busy) / float64(busy+idle) * 100, true
}

// busyShareReading reads the share of processor time spent working since its previous reading;
// its first spans firstBusyWindow on the clock and each later one at least minBusyWindow.
func busyShareReading(clock Clock, readStat func() (string, error)) processorReading {
	var last *cpuTimes
	var lastAt time.Time
	sample := func() (cpuTimes, bool) {
		raw, err := readStat()
		if err != nil {
			return cpuTimes{}, false
		}
		return parseCPUTimes(raw)
	}
	return func() (float64, bool) {
		if last == nil {
			first, ok := sample()
			if !ok {
				return 0, false
			}
			last = &first
			clock.Sleep(firstBusyWindow)
		} else if since := clock.Now().Sub(lastAt); since < minBusyWindow {
			clock.Sleep(minBusyWindow - since)
		}
		now, ok := sample()
		if !ok {
			return 0, false
		}
		lastAt = clock.Now()
		percent, ok := busyPercent(*last, now)
		last = &now
		return percent, ok
	}
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
