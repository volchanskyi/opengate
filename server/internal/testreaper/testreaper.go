// Package testreaper sets the testcontainers reaper's wait and session for a busy machine.
// It imports no internal package, so any provisioning helper can call it.
package testreaper

import (
	"os"
	"strings"

	"github.com/google/uuid"
)

const (
	// The reaper reads timeoutEnv as how long it waits for a client before reaping every container.
	timeoutEnv = "TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"
	timeout    = "180s"

	// sessionEnv selects the reaper container; a per-process value gives each process its own reaper.
	sessionEnv = "TESTCONTAINERS_SESSION_ID"
)

// Settle applies both reaper settings; call it from init before any container starts.
func Settle() {
	widenWait()
	isolateSession()
}

// widenWait sets the reaper wait unless the operator already set it.
func widenWait() {
	if os.Getenv(timeoutEnv) == "" {
		_ = os.Setenv(timeoutEnv, timeout)
	}
}

// isolateSession gives this process its own reaper unless the operator set the session.
func isolateSession() {
	if os.Getenv(sessionEnv) == "" {
		_ = os.Setenv(sessionEnv, newSessionID())
	}
}

// newSessionID returns a unique 64-character hex value that fits a Docker container name.
func newSessionID() string {
	return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
}
