package app

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keepMemoryLimit restores the process's memory limit once the test ends.
func keepMemoryLimit(t *testing.T) {
	t.Helper()
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })
}

func cgroupFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.max")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestGoKeepsItsHeapToNinetyPercentOfTheContainersLimit(t *testing.T) {
	keepMemoryLimit(t)
	t.Setenv("GOMEMLIMIT", "")
	require.NoError(t, os.Unsetenv("GOMEMLIMIT"))

	set, err := ApplyMemoryLimit(cgroupFile(t, "402653184\n"))

	require.NoError(t, err)
	assert.Equal(t, int64(362387865), set, "a 384 MiB container leaves Go 90% of it")
	assert.Equal(t, set, debug.SetMemoryLimit(-1))
}

func TestAnExplicitGoMemLimitWins(t *testing.T) {
	keepMemoryLimit(t)
	t.Setenv("GOMEMLIMIT", "200MiB")
	before := debug.SetMemoryLimit(-1)

	set, err := ApplyMemoryLimit(cgroupFile(t, "402653184\n"))

	require.NoError(t, err)
	assert.Zero(t, set)
	assert.Equal(t, before, debug.SetMemoryLimit(-1), "the operator's own limit is left alone")
}

func TestAContainerWithNoLimitLeavesGoUnlimited(t *testing.T) {
	keepMemoryLimit(t)
	t.Setenv("GOMEMLIMIT", "")
	require.NoError(t, os.Unsetenv("GOMEMLIMIT"))
	before := debug.SetMemoryLimit(-1)

	unlimited := []string{
		cgroupFile(t, "max\n"),
		filepath.Join(t.TempDir(), "absent"),
		filepath.Join(t.TempDir(), "no-cgroup", "memory.max"),
	}
	for _, path := range unlimited {
		set, err := ApplyMemoryLimit(path)
		require.NoError(t, err)
		assert.Zero(t, set)
	}
	assert.Equal(t, before, debug.SetMemoryLimit(-1))
}

func TestAnUnreadableLimitIsAnError(t *testing.T) {
	keepMemoryLimit(t)
	t.Setenv("GOMEMLIMIT", "")
	require.NoError(t, os.Unsetenv("GOMEMLIMIT"))
	_, err := ApplyMemoryLimit(cgroupFile(t, "lots\n"))
	require.Error(t, err)
}
