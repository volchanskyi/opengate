package testpg

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWidenReaperWaitLeavesAnOperatorsChoiceAlone pins both halves of the
// reaper setting. Without it the whole test process fails on a busy machine
// with a message about a reaper nobody wrote; with it overriding somebody's
// deliberate value, a shorter wait they chose would be silently ignored.
func TestWidenReaperWaitLeavesAnOperatorsChoiceAlone(t *testing.T) {
	t.Setenv(ryukTimeoutEnv, "5s")
	widenReaperWait()
	assert.Equal(t, "5s", os.Getenv(ryukTimeoutEnv), "an operator's own value stands")

	t.Setenv(ryukTimeoutEnv, "")
	widenReaperWait()
	assert.Equal(t, ryukTimeout, os.Getenv(ryukTimeoutEnv),
		"an unset wait takes the widened default, or a busy machine fails on the reaper")
}
