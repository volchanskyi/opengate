package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// techniciansMaySignInAnnouncement is the line the run prints once its administrator exists;
// scripts/loadtest-k6-alongside.sh signs the technicians in when it reads it.
const techniciansMaySignInAnnouncement = "Technicians may sign in"

// techniciansSignedIn is what the ready file holds when every technician signed in.
const techniciansSignedIn = "signed-in"

// techniciansWait is how long the walk waits for the technicians to sign in.
const techniciansWait = 10 * time.Minute

// techniciansPoll is how often the run looks for the ready file.
const techniciansPoll = time.Second

// announceTechnicians tells the technicians' step it may sign in, where the run waits for one.
func announceTechnicians(path string) {
	if path != "" {
		fmt.Println(techniciansMaySignInAnnouncement)
	}
}

// awaitTechnicians waits up to limit for path to read signed-in, and stops in the step's own words
// when it reads anything else; an empty path waits for nothing.
func awaitTechnicians(path string, limit time.Duration, clock Clock) error {
	if path == "" {
		return nil
	}
	deadline := clock.Now().Add(limit)
	for {
		written, err := os.ReadFile(filepath.Clean(path))
		said := strings.TrimSpace(string(written))
		switch {
		case err == nil && said == techniciansSignedIn:
			return nil
		case err == nil && said != "":
			return fmt.Errorf("the technicians did not sign in: %s", said)
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("reading the technicians' ready file: %w", err)
		}
		if !clock.Now().Before(deadline) {
			return fmt.Errorf("the technicians were not signed in within %s, so the walk never started: %s was never written",
				limit, path)
		}
		clock.Sleep(techniciansPoll)
	}
}
