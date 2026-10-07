package main

import (
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	headroomScopeGenerator = "generator"
	headroomScopeMachine   = "machine"
)

const machineSampleInterval = 5 * time.Second

const (
	cgroupRoot     = "/sys/fs/cgroup"
	procSelfCgroup = "/proc/self/cgroup"
)

// GeneratorAllowance is the processor count and memory ceiling the generator may use.
type GeneratorAllowance struct {
	Processors  float64
	MemoryBytes int64
}

// GeneratorMeter watches the generator while it produces load and reports one reading.
type GeneratorMeter interface {
	// Sample takes one look at the generator's memory and processor use.
	Sample()
	// Stop takes the last look and reports the run's headroom.
	Stop() Headroom
}

// StartGeneratorMeter measures against the process's own cgroup allowance, else the machine.
func StartGeneratorMeter() GeneratorMeter {
	if files, ok := openOwnCgroup(); ok {
		if meter := startCgroupMeter(files, time.Now); meter != nil {
			return meter
		}
		files.close()
	}
	return startMachineMeter(LocalNodeReading)
}

func runHasItsOwnAllowance() bool {
	files, ok := openOwnCgroup()
	if !ok {
		return false
	}
	defer files.close()
	_, granted := readGeneratorAllowance(files)
	return granted
}

// WatchGenerator samples the generator on an interval and returns a stop function that reports.
func WatchGenerator() func() Headroom {
	meter := StartGeneratorMeter()
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(machineSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				meter.Sample()
			}
		}
	}()
	return func() Headroom {
		close(done)
		<-finished
		return meter.Stop()
	}
}

type cgroupCPU struct {
	UsageMicros int64
	// RefusedMicros is throttled time; Refusals reports whether the kernel keeps that account.
	RefusedMicros int64
	Refusals      bool
}

// cgroupFiles scopes every read to the mount, so a path taken from /proc/self/cgroup
// cannot reach outside the hierarchy.
type cgroupFiles struct {
	root *os.Root
	dir  string
}

func openCgroupFiles(mount, dir string) (cgroupFiles, bool) {
	root, err := os.OpenRoot(mount)
	if err != nil {
		return cgroupFiles{}, false
	}
	files := cgroupFiles{root: root, dir: dir}
	if _, err := files.read("cpu.max"); err != nil {
		files.close()
		return cgroupFiles{}, false
	}
	return files, true
}

func (c cgroupFiles) read(name string) ([]byte, error) {
	return c.root.ReadFile(path.Join(c.dir, name))
}

func (c cgroupFiles) close() {
	if c.root != nil {
		_ = c.root.Close()
	}
}

type cgroupMeter struct {
	files     cgroupFiles
	now       func() time.Time
	allowance GeneratorAllowance

	startedAt time.Time
	start     cgroupCPU
	peakBytes int64
}

func startCgroupMeter(files cgroupFiles, now func() time.Time) *cgroupMeter {
	allowance, ok := readGeneratorAllowance(files)
	if !ok {
		return nil
	}
	cpu, ok := readCgroupCPU(files)
	if !ok {
		return nil
	}
	return &cgroupMeter{
		files: files, now: now, allowance: allowance,
		startedAt: now(), start: cpu,
		peakBytes: readInt64Account(files, "memory.current"),
	}
}

func (m *cgroupMeter) Sample() {
	if held := readInt64Account(m.files, "memory.current"); held > m.peakBytes {
		m.peakBytes = held
	}
}

func (m *cgroupMeter) Stop() Headroom {
	m.Sample()
	defer m.files.close()

	end, ok := readCgroupCPU(m.files)
	elapsed := m.now().Sub(m.startedAt)
	if !ok || elapsed <= 0 || m.allowance.Processors <= 0 {
		return Headroom{}
	}

	headroom, refused := roomOver(m.start, end, elapsed, m.allowance.Processors)
	reading := Headroom{
		Measured:           true,
		Scope:              headroomScopeGenerator,
		CPUHeadroomPercent: headroom,
		CPURefusedPercent:  refused,
	}
	if m.allowance.MemoryBytes > 0 {
		reading.MemoryUsedPercent = clampPercent(float64(m.peakBytes) / float64(m.allowance.MemoryBytes) * 100)
	}
	return reading
}

// roomOver returns the unused share of the allowance and the refused share of the window;
// the refused share is nil where the kernel keeps no refusal account.
func roomOver(before, after cgroupCPU, window time.Duration, processors float64) (float64, *float64) {
	offered := window.Seconds() * processors * 1e6
	used := float64(after.UsageMicros-before.UsageMicros) / offered * 100
	if !before.Refusals || !after.Refusals {
		return clampPercent(100 - used), nil
	}
	refused := clampPercent(float64(after.RefusedMicros-before.RefusedMicros) / (window.Seconds() * 1e6) * 100)
	return clampPercent(100 - used), &refused
}

type machineMeter struct {
	read     func() NodeReading
	samples  int
	cpuTotal float64
	peakMem  float64
}

func startMachineMeter(read func() NodeReading) *machineMeter {
	meter := &machineMeter{read: read}
	meter.Sample()
	return meter
}

func (m *machineMeter) Sample() {
	reading := m.read()
	if !reading.Measured {
		return
	}
	m.samples++
	m.cpuTotal += reading.CPUPercent
	if reading.MemoryPercent > m.peakMem {
		m.peakMem = reading.MemoryPercent
	}
}

// Stop reports the mean CPU commitment across the run; memory keeps its peak, which decides
// whether the run had room.
func (m *machineMeter) Stop() Headroom {
	m.Sample()
	if m.samples == 0 {
		return Headroom{}
	}
	return Headroom{
		Measured:           true,
		Scope:              headroomScopeMachine,
		CPUHeadroomPercent: clampPercent(100 - m.cpuTotal/float64(m.samples)),
		MemoryUsedPercent:  m.peakMem,
	}
}

// readGeneratorAllowance reports no allowance for a cgroup without a processor quota.
func readGeneratorAllowance(files cgroupFiles) (GeneratorAllowance, bool) {
	raw, err := files.read("cpu.max")
	if err != nil {
		return GeneratorAllowance{}, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 || fields[0] == "max" {
		return GeneratorAllowance{}, false
	}
	quota, quotaErr := strconv.ParseFloat(fields[0], 64)
	period, periodErr := strconv.ParseFloat(fields[1], 64)
	if quotaErr != nil || periodErr != nil || period <= 0 || quota <= 0 {
		return GeneratorAllowance{}, false
	}

	allowance := GeneratorAllowance{Processors: quota / period}
	// A memory ceiling of "max" leaves MemoryBytes zero.
	if limit := readInt64Account(files, "memory.max"); limit > 0 {
		allowance.MemoryBytes = limit
	}
	return allowance, true
}

func readCgroupCPU(files cgroupFiles) (cgroupCPU, bool) {
	raw, err := files.read("cpu.stat")
	if err != nil {
		return cgroupCPU{}, false
	}
	var cpu cgroupCPU
	var sawUsage bool
	for line := range strings.SplitSeq(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "usage_usec":
			cpu.UsageMicros = value
			sawUsage = true
		case "throttled_usec":
			cpu.RefusedMicros = value
			cpu.Refusals = true
		}
	}
	return cpu, sawUsage
}

// openOwnCgroup tries the path from /proc/self/cgroup, then the mount root itself, which is
// the process's cgroup inside a cgroup namespace.
func openOwnCgroup() (cgroupFiles, bool) {
	for _, dir := range ownCgroupCandidates() {
		if files, ok := openCgroupFiles(cgroupRoot, dir); ok {
			return files, true
		}
	}
	return cgroupFiles{}, false
}

func ownCgroupCandidates() []string {
	candidates := []string{"."}
	raw, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return candidates
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		// The unified hierarchy's line is "0::<path>".
		own, found := strings.CutPrefix(strings.TrimSpace(line), "0::")
		if !found {
			continue
		}
		if own = strings.TrimPrefix(own, "/"); own != "" {
			candidates = append([]string{own}, candidates...)
		}
	}
	return candidates
}

// readInt64Account returns zero where the account is absent or holds a non-number such as "max".
func readInt64Account(files cgroupFiles, name string) int64 {
	raw, err := files.read(name)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// clampPercent holds a share between 0 and 100; slight overuse of an allowance leaves no
// negative room.
func clampPercent(value float64) float64 {
	return math.Min(math.Max(value, 0), 100)
}
