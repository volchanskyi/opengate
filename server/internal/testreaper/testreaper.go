// Package testreaper settles the two testcontainers reaper settings that decide
// whether a container-provisioning test package survives a busy machine.
//
// Every package that starts a container through testcontainers depends on the
// Ryuk reaper, and takes two defaults that do not hold under this suite. Settle
// replaces both. It is a leaf package — it imports no internal/* package — so
// any provisioning helper can call it without risking an import cycle.
package testreaper

import (
	"os"
	"strings"

	"github.com/google/uuid"
)

const (
	// timeoutEnv and timeout are read by the container reaper itself: they set
	// how long it waits for a client to connect before it shuts down and reaps
	// everything it holds. Its own default is 60s, and a workstation running the
	// gauntlet — a Rust build, a Go suite, a browser stack and several databases
	// at once — takes longer than that to reach the reaper. It then tears down
	// the containers the suite is still using. Waiting longer costs nothing when
	// the machine is idle.
	timeoutEnv = "TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT"
	timeout    = "180s"

	// sessionEnv decides which reaper container a process belongs to. Left
	// alone, testcontainers derives it from the parent process, so every package
	// process under one `go test ./...` shares a single reaper: the first to
	// need one creates it and every other waits on that container to report
	// ready. That wait is a fixed sixty seconds with no setting behind it. On a
	// machine busy enough for the container to be slow — or one where it has
	// already reaped itself and gone — the wait runs out, and the package left
	// holding it fails on a reaper it never started, naming a container nobody
	// wrote. A session of this process's own means it creates its own reaper and
	// waits on nothing anybody else owns.
	sessionEnv = "TESTCONTAINERS_SESSION_ID"
)

// Settle applies both reaper settings. Call it from the init of any package
// that provisions a container, before anything can create one: a package that
// provisions nothing on some runs still shares the process with one that does,
// and whichever starts the first container takes whatever the environment holds
// at that moment.
func Settle() {
	widenWait()
	isolateSession()
}

// widenWait gives the container reaper room to see a slow suite out. An
// operator who has set the variable themselves keeps their value.
func widenWait() {
	if os.Getenv(timeoutEnv) == "" {
		_ = os.Setenv(timeoutEnv, timeout)
	}
}

// isolateSession gives this process a reaper of its own, so it never waits on a
// container another process created and owns. An operator who has set the
// variable themselves keeps their value.
func isolateSession() {
	if os.Getenv(sessionEnv) == "" {
		_ = os.Setenv(sessionEnv, newSessionID())
	}
}

// newSessionID returns a value no other process produces, in the 64-character
// hex shape testcontainers builds the reaper's container name out of.
func newSessionID() string {
	return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
}
