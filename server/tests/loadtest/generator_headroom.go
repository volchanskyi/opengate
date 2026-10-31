package main

import (
	"math"
	"time"
)

const (
	headroomScopeGenerator = "generator"
	headroomScopeMachine   = "machine"
)

const machineSampleInterval = 5 * time.Second

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

// clampPercent holds a share between 0 and 100; slight overuse of an allowance leaves no
// negative room.
func clampPercent(value float64) float64 {
	return math.Min(math.Max(value, 0), 100)
}
