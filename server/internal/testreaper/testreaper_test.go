package testreaper

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWidenWaitLeavesAnOperatorsChoiceAlone(t *testing.T) {
	t.Setenv(timeoutEnv, "5s")
	widenWait()
	assert.Equal(t, "5s", os.Getenv(timeoutEnv), "an operator's own value stands")

	t.Setenv(timeoutEnv, "")
	widenWait()
	assert.Equal(t, timeout, os.Getenv(timeoutEnv),
		"an unset wait takes the widened default, or a busy machine fails on the reaper")
}

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

func TestSessionsDoNotCollide(t *testing.T) {
	assert.NotEqual(t, newSessionID(), newSessionID(),
		"two sessions collided, so two processes would wait on one reaper")
}

func TestSessionIDNamesAContainer(t *testing.T) {
	id := newSessionID()
	assert.Len(t, id, 64, "the reaper's container name is built from this")
	for _, r := range id {
		assert.True(t,
			(r >= '0' && r <= '9') || (r >= 'a' && r <= 'f'),
			"session id carries %q, which is not hex", r)
	}
}

func TestSettleAppliesBoth(t *testing.T) {
	t.Setenv(timeoutEnv, "")
	t.Setenv(sessionEnv, "")
	Settle()
	assert.Equal(t, timeout, os.Getenv(timeoutEnv), "the wait is widened")
	assert.NotEmpty(t, os.Getenv(sessionEnv), "the session is this process's own")
}
