package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the generator had left, and where that figure comes from.
//
// It was one look at the machine's run queue, taken after the fleet had already
// been wound down, and it decided whether a whole night's numbers counted. Two
// legs of a five-leg sweep came back at nought percent and three did not, on
// runs that connected every machine they asked for — the difference between
// them was which instant the sample landed on. Inside a pod the same file
// describes the node, so a nightly on a shared machine read production's load
// as its own generator's.

func writeCgroup(t *testing.T, accounts map[string]string) (string, cgroupFiles) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range accounts {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}
	files, ok := openCgroupFiles(dir, ".")
	require.True(t, ok, "the fixture is a readable cgroup")
	t.Cleanup(files.close)
	return dir, files
}

// A generator with its own allowance is measured against that allowance, so the
// figure describes the generator rather than whatever else the box is carrying.
func TestAGeneratorWithItsOwnAllowanceIsMeasuredAgainstIt(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":        "100000 100000\n",
		"cpu.stat":       "usage_usec 0\nthrottled_usec 0\n",
		"memory.current": "104857600\n",
		"memory.max":     "536870912\n",
	})

	allowance, ok := readGeneratorAllowance(files)
	require.True(t, ok, "a cgroup with a quota is an allowance")
	assert.InDelta(t, 1.0, allowance.Processors, 0.001, "100000 of a 100000 period is one processor")
	assert.EqualValues(t, 536870912, allowance.MemoryBytes)
}

// A cgroup with no quota is not an allowance. The generator shares the box with
// whatever else is on it, and saying so is the finding.
func TestACgroupWithNoQuotaIsNotAnAllowance(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":        "max 100000\n",
		"cpu.stat":       "usage_usec 0\n",
		"memory.current": "104857600\n",
		"memory.max":     "max\n",
	})

	_, ok := readGeneratorAllowance(files)
	assert.False(t, ok, "a quota of max is no quota at all")
}

// The reading is the difference between two looks, taken either side of the
// load. One look says what the box was doing at one instant, which for a
// fifteen-minute run is a coin toss.
func TestHeadroomIsTheDifferenceAcrossTheRun(t *testing.T) {
	dir, files := writeCgroup(t, map[string]string{
		"cpu.max":        "200000 100000\n",
		"cpu.stat":       "usage_usec 1000000\nthrottled_usec 0\n",
		"memory.current": "104857600\n",
		"memory.max":     "1073741824\n",
	})

	at := time.Unix(1_800_000_000, 0)
	meter := startCgroupMeter(files, func() time.Time { return at })
	require.NotNil(t, meter)

	// Ten seconds of wall clock, in which the generator spent twelve processor
	// seconds of the twenty its two-processor allowance offers.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cpu.stat"),
		[]byte("usage_usec 13000000\nthrottled_usec 0\n"), 0o600))
	at = at.Add(10 * time.Second)

	reading := meter.Stop()
	require.True(t, reading.Measured)
	assert.Equal(t, headroomScopeGenerator, reading.Scope)
	assert.InDelta(t, 40.0, reading.CPUHeadroomPercent, 0.5,
		"twelve processor-seconds of twenty leaves two fifths")
	assert.InDelta(t, 9.77, reading.MemoryUsedPercent, 0.1)
}

// A generator refused the processor measured its own wait into every latency it
// reported. The refusal is the kernel's own account of it.
func TestTimeTheGeneratorWasRefusedTheProcessorIsRead(t *testing.T) {
	dir, files := writeCgroup(t, map[string]string{
		"cpu.max":        "100000 100000\n",
		"cpu.stat":       "usage_usec 0\nthrottled_usec 0\n",
		"memory.current": "1\n",
		"memory.max":     "1000\n",
	})

	at := time.Unix(1_800_000_000, 0)
	meter := startCgroupMeter(files, func() time.Time { return at })
	require.NotNil(t, meter)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "cpu.stat"),
		[]byte("usage_usec 2000000\nthrottled_usec 3000000\n"), 0o600))
	at = at.Add(10 * time.Second)

	reading := meter.Stop()
	require.NotNil(t, reading.CPURefusedPercent, "a kernel that counts the refusals reports them")
	assert.InDelta(t, 30.0, *reading.CPURefusedPercent, 0.5,
		"three seconds refused out of ten is three tenths of the run")
}

// A kernel that does not count refusals reports none rather than nought: nought
// is a generator that was never kept waiting, and this is not that.
func TestAKernelThatCountsNoRefusalsReportsNone(t *testing.T) {
	_, files := writeCgroup(t, map[string]string{
		"cpu.max":        "100000 100000\n",
		"cpu.stat":       "usage_usec 0\n",
		"memory.current": "1\n",
		"memory.max":     "1000\n",
	})

	at := time.Unix(1_800_000_000, 0)
	meter := startCgroupMeter(files, func() time.Time { return at })
	require.NotNil(t, meter)
	at = at.Add(10 * time.Second)

	assert.Nil(t, meter.Stop().CPURefusedPercent)
}

// Where the generator has no allowance of its own it shares the box with the
// system it is measuring, and the reading is of the box. It says so, so that
// nothing reads a busy box as a starved generator — the throwaway venue exists
// precisely to drive that box hard.
func TestAGeneratorSharingABoxSaysTheReadingIsOfTheBox(t *testing.T) {
	// Two looks bracketing the load: one when the meter starts, one when it
	// stops. A run long enough to matter takes many more in between.
	readings := []NodeReading{
		{Measured: true, CPUPercent: 40, MemoryPercent: 30},
		{Measured: true, CPUPercent: 60, MemoryPercent: 50},
	}
	var next int
	meter := startMachineMeter(func() NodeReading {
		reading := readings[min(next, len(readings)-1)]
		next++
		return reading
	})

	reading := meter.Stop()

	require.True(t, reading.Measured)
	assert.Equal(t, headroomScopeMachine, reading.Scope)
	assert.InDelta(t, 50.0, reading.CPUHeadroomPercent, 0.001,
		"the mean commitment across the run, not whichever instant the last look landed on")
	assert.InDelta(t, 50.0, reading.MemoryUsedPercent, 0.001, "the most it ever held")
	assert.Nil(t, reading.CPURefusedPercent, "a box does not account for one process's waits")
}

// A machine that could not be read at all is not a machine with room.
func TestAMachineThatCannotBeReadReportsNoHeadroom(t *testing.T) {
	meter := startMachineMeter(func() NodeReading { return NodeReading{} })
	meter.Sample()
	assert.False(t, meter.Stop().Measured)
}
