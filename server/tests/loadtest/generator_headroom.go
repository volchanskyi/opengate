package main

import (
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// What the generator had left while it was producing the load.
//
// A latency figure is a property of the pair. The same server measured from a
// generator with nothing left is a different number, and the difference is
// entirely about the generator — so a run that cannot say how much room its own
// generator had cannot say what its numbers are about.
//
// Two things decide what that sentence can honestly mean, and both are read
// here. The first is *when*: one look at a machine says what it was doing at
// one instant, and a fifteen-minute run divided by one instant is a coin toss —
// two legs of a five-leg sweep came back at nought percent and three did not,
// on runs that had all connected every machine they asked for. So the reading
// brackets the load rather than sampling the end of it.
//
// The second is *whose*. A generator with an allowance of its own — a quota on
// its own cgroup — can be measured against that allowance, and the answer is
// about the generator. A generator sharing a box with the system it is
// measuring cannot: what the box has left is what the two of them have left
// together, and on the throwaway venue driving that box hard is the experiment
// rather than a hazard. So the reading says which of the two it is, and the
// rule that invalidates a run falls only on the first.

// Headroom scopes. A reading names what it describes, because the same numbers
// mean different things depending on whose they are.
const (
	// headroomScopeGenerator is the generator measured against an allowance of
	// its own.
	headroomScopeGenerator = "generator"
	// headroomScopeMachine is the box the generator shares with the system
	// under test, which is what the throwaway venue is.
	headroomScopeMachine = "machine"
)

// machineSampleInterval is how often the box is looked at while it carries the
// load. Frequent enough that a phase-long squeeze cannot hide between two looks,
// rare enough that the watching is not itself a load.
const machineSampleInterval = 5 * time.Second

// cgroupRoot is where a cgroup v2 hierarchy is mounted, and procSelfCgroup is
// where the kernel says which part of it this process sits in.
const (
	cgroupRoot     = "/sys/fs/cgroup"
	procSelfCgroup = "/proc/self/cgroup"
)

// GeneratorAllowance is what the generator is permitted to use. A generator
// without one shares whatever the box has.
type GeneratorAllowance struct {
	Processors  float64
	MemoryBytes int64
}

// GeneratorMeter watches the generator for as long as it produces load, and
// reports one reading of the whole of it.
type GeneratorMeter interface {
	// Sample takes one look. A meter that reads a running total ignores it.
	Sample()
	// Stop takes the last look and reports what the run had.
	Stop() Headroom
}

// StartGeneratorMeter begins watching this machine, from its own allowance
// where the kernel gives it one and from the box otherwise.
//
// The box is only ever reached on the venue where the generator shares it with
// the stack it drives, so the instant reading LocalNodeReading takes is the
// right one there for the same reason it is the right one for that venue's
// safety ceilings.
func StartGeneratorMeter() GeneratorMeter {
	if files, ok := openOwnCgroup(); ok {
		if meter := startCgroupMeter(files, time.Now); meter != nil {
			return meter
		}
		files.close()
	}
	return startMachineMeter(LocalNodeReading)
}

// runHasItsOwnAllowance reports whether the kernel gives this process a
// processor allowance of its own — a quota on its own cgroup.
//
// It is what being a guest on somebody else's machine looks like from inside,
// and it is the same question that decides whose room the generator's own
// reading describes. Confirmed on both venues: the staging load-test pod's 400
// millicores arrive as `cpu.max 40000 100000`, and every leg of the throwaway
// perf stack reports the machine scope, which is this answering no.
func runHasItsOwnAllowance() bool {
	files, ok := openOwnCgroup()
	if !ok {
		return false
	}
	defer files.close()
	_, granted := readGeneratorAllowance(files)
	return granted
}

// WatchGenerator samples the generator on an interval until stop is called,
// then reports. It is what a run uses: the load runs between the two calls, and
// the reading is of the load rather than of whatever the box was doing when the
// last machine hung up.
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

// cgroupCPU is the kernel's running account of one cgroup's processor use.
type cgroupCPU struct {
	UsageMicros int64
	// RefusedMicros is time the cgroup's tasks were runnable and denied the
	// processor. Refusals reports whether the kernel keeps that account at all;
	// a kernel that does not is silent rather than reporting none.
	RefusedMicros int64
	Refusals      bool
}

// cgroupFiles is one cgroup's accounts, opened beneath the hierarchy's own
// mount point.
//
// The directory inside it comes out of /proc/self/cgroup, which is a path this
// process did not choose. Scoping every read to the mount is what makes that
// safe: a name from that file cannot reach anything outside the hierarchy it
// belongs to, whatever it says.
type cgroupFiles struct {
	root *os.Root
	dir  string
}

// openCgroupFiles opens the accounts at dir beneath mount, and reports nothing
// where there is no processor account to read — which is how a machine with no
// cgroup hierarchy, and a hierarchy this process is not inside, both answer.
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

// read reads one account.
func (c cgroupFiles) read(name string) ([]byte, error) {
	return c.root.ReadFile(path.Join(c.dir, name))
}

// close lets go of the mount.
func (c cgroupFiles) close() {
	if c.root != nil {
		_ = c.root.Close()
	}
}

// cgroupMeter measures the generator against its own allowance, by the two
// accounts the kernel keeps for it: the processor time it has spent, and the
// time it was runnable and refused.
type cgroupMeter struct {
	files     cgroupFiles
	now       func() time.Time
	allowance GeneratorAllowance

	startedAt time.Time
	start     cgroupCPU
	peakBytes int64
}

// startCgroupMeter begins a reading against the allowance these accounts grant,
// or reports nothing where they grant none.
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

// Sample keeps the most memory the generator has been seen holding. The
// processor accounts are running totals, so they need no sampling — the
// difference between the two ends is the whole run.
func (m *cgroupMeter) Sample() {
	if held := readInt64Account(m.files, "memory.current"); held > m.peakBytes {
		m.peakBytes = held
	}
}

// Stop reports what the generator had while it produced the load.
func (m *cgroupMeter) Stop() Headroom {
	m.Sample()
	defer m.files.close()

	end, ok := readCgroupCPU(m.files)
	elapsed := m.now().Sub(m.startedAt)
	if !ok || elapsed <= 0 || m.allowance.Processors <= 0 {
		return Headroom{}
	}

	// The processor time the allowance offered over this window, against what
	// the generator actually spent of it.
	offered := elapsed.Seconds() * m.allowance.Processors * 1e6
	used := float64(end.UsageMicros-m.start.UsageMicros) / offered * 100

	reading := Headroom{
		Measured:           true,
		Scope:              headroomScopeGenerator,
		CPUHeadroomPercent: clampPercent(100 - used),
	}
	if m.allowance.MemoryBytes > 0 {
		reading.MemoryUsedPercent = clampPercent(float64(m.peakBytes) / float64(m.allowance.MemoryBytes) * 100)
	}
	if end.Refusals && m.start.Refusals {
		refused := clampPercent(float64(end.RefusedMicros-m.start.RefusedMicros) / (elapsed.Seconds() * 1e6) * 100)
		reading.CPURefusedPercent = &refused
	}
	return reading
}

// machineMeter measures the box the generator shares with the system it is
// measuring. It is a reading of the box and says so.
type machineMeter struct {
	read     func() NodeReading
	samples  int
	cpuTotal float64
	peakMem  float64
}

// startMachineMeter begins watching the box through the given reader.
func startMachineMeter(read func() NodeReading) *machineMeter {
	meter := &machineMeter{read: read}
	meter.Sample()
	return meter
}

// Sample takes one look at the box.
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

// Stop reports the box's mean commitment across the run and the most memory it
// was ever holding.
//
// The mean rather than the worst, because one busy instant in a fifteen-minute
// run is what the single look this replaces was already reporting. The worst is
// kept for memory, where the peak is the figure that decides whether the run had
// room at all.
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

// readGeneratorAllowance reads what a cgroup permits. A cgroup with no
// processor quota grants no allowance: the generator shares whatever the box
// has, which is a different statement and is reported as one.
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
	// A memory ceiling of "max" is no ceiling; the figure stays absent rather
	// than being filled in with the machine's, which would describe something
	// else.
	if limit := readInt64Account(files, "memory.max"); limit > 0 {
		allowance.MemoryBytes = limit
	}
	return allowance, true
}

// readCgroupCPU reads the kernel's running processor accounts for one cgroup.
func readCgroupCPU(files cgroupFiles) (cgroupCPU, bool) {
	raw, err := files.read("cpu.stat")
	if err != nil {
		return cgroupCPU{}, false
	}
	var cpu cgroupCPU
	var sawUsage bool
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

// openOwnCgroup opens this process's own cgroup accounts, or reports nothing
// where there are none.
//
// A container with a cgroup namespace sees its own cgroup as the root, so the
// mount point itself is the answer there; without one, /proc/self/cgroup names
// where in the hierarchy this process sits. Both are tried, nearest first, and
// every read is scoped to the mount.
func openOwnCgroup() (cgroupFiles, bool) {
	for _, dir := range ownCgroupCandidates() {
		if files, ok := openCgroupFiles(cgroupRoot, dir); ok {
			return files, true
		}
	}
	return cgroupFiles{}, false
}

// ownCgroupCandidates is where this process's accounts might sit, relative to
// the hierarchy's mount point.
func ownCgroupCandidates() []string {
	candidates := []string{"."}
	raw, err := os.ReadFile(procSelfCgroup)
	if err != nil {
		return candidates
	}
	for _, line := range strings.Split(string(raw), "\n") {
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

// readInt64Account reads a single number out of one cgroup account, or zero
// where it is absent or holds something else — "max", most often, which is a
// ceiling that is not one.
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

// clampPercent holds a share inside nought and a hundred. A generator that spent
// marginally more than its allowance across a window whose ends were read
// microseconds apart from the accounts has no negative amount of room.
func clampPercent(value float64) float64 {
	return math.Min(math.Max(value, 0), 100)
}
