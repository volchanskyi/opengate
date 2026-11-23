package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cpuMaxFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cpu.max")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestTheProcessorShareIsTheContainersQuotaOverItsPeriod(t *testing.T) {
	t.Parallel()

	share, err := ReadProcessorShare(cpuMaxFile(t, "25000 100000\n"))

	require.NoError(t, err)
	assert.InDelta(t, 0.25, share, 1e-9, "production's 250m is a quarter processor")
}

func TestAContainerWithNoProcessorLimitHasNoShare(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		cpuMaxFile(t, "max 100000\n"),
		filepath.Join(t.TempDir(), "absent"),
	} {
		share, err := ReadProcessorShare(path)
		require.NoError(t, err)
		assert.Zero(t, share, "%s sets no share, so the machine's processors stand", path)
	}
}

func TestAnUnreadableProcessorLimitIsAnError(t *testing.T) {
	t.Parallel()

	for _, content := range []string{"quarter 100000\n", "25000 0\n", "25000\n"} {
		_, err := ReadProcessorShare(cpuMaxFile(t, content))
		assert.Error(t, err, "%q is no quota and period", content)
	}
}
