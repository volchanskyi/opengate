package testpg

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestWidenReaperWaitLeavesAnOperatorsChoiceAlone pins both halves of the
// reaper setting. Without it the reaper shuts down and reaps mid-suite on a
// machine slow enough to need more than its default minute; with it overriding
// somebody's deliberate value, a shorter wait they chose would be silently
// ignored.
func TestWidenReaperWaitLeavesAnOperatorsChoiceAlone(t *testing.T) {
	t.Setenv(ryukTimeoutEnv, "5s")
	widenReaperWait()
	assert.Equal(t, "5s", os.Getenv(ryukTimeoutEnv), "an operator's own value stands")

	t.Setenv(ryukTimeoutEnv, "")
	widenReaperWait()
	assert.Equal(t, ryukTimeout, os.Getenv(ryukTimeoutEnv),
		"an unset wait takes the widened default, or a busy machine fails on the reaper")
}

// TestIsolateSessionLeavesAnOperatorsChoiceAlone is the same contract for the
// session, which decides whose reaper this process waits on.
func TestIsolateSessionLeavesAnOperatorsChoiceAlone(t *testing.T) {
	t.Setenv(sessionEnv, "an-operators-own-session")
	isolateSession()
	assert.Equal(t, "an-operators-own-session", os.Getenv(sessionEnv),
		"an operator's own session stands")

	t.Setenv(sessionEnv, "")
	isolateSession()
	assert.NotEmpty(t, os.Getenv(sessionEnv),
		"an unset session takes one of this process's own")
}

// TestSessionsDoNotCollide is the whole point of the isolation: two processes
// landing on one session share a reaper container, and every process but the
// one that created it then waits on it for a fixed minute it cannot extend.
func TestSessionsDoNotCollide(t *testing.T) {
	assert.NotEqual(t, newSessionID(), newSessionID(),
		"two sessions collided, so two processes would wait on one reaper")
}

// TestSessionIDNamesAContainer holds the shape testcontainers builds the
// reaper's container name out of: a Docker name accepts letters, digits and a
// few separators, and nothing else.
func TestSessionIDNamesAContainer(t *testing.T) {
	id := newSessionID()
	assert.Len(t, id, 64, "the reaper's container name is built from this")
	for _, r := range id {
		assert.True(t,
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'),
			"session id carries %q, which is not hex", r)
	}
}
