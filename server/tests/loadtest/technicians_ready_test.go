package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writingClock writes body to path on its nth sleep, as the technicians' step does mid-wait.
type writingClock struct {
	testClock
	path   string
	body   string
	nth    int
	sleeps int
}

func (c *writingClock) Sleep(d time.Duration) {
	c.sleeps++
	if c.sleeps == c.nth {
		_ = os.WriteFile(c.path, []byte(c.body), 0o600)
	}
	c.testClock.Sleep(d)
}

func TestTheWalkWaitsForTheTechniciansReadyFile(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		nth     int
		refusal string
	}{
		{name: "signed in after a few looks", body: "signed-in\n", nth: 3},
		{name: "signed in before the first look", body: "signed-in", nth: 0},
		{name: "the step says why it failed", body: "refused: register returned 503\n", nth: 2,
			refusal: "the technicians did not sign in: refused: register returned 503"},
		{name: "never written", nth: -1,
			refusal: "the technicians were not signed in within 10m0s, so the walk never started"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "technicians.ready")
			if tc.nth == 0 {
				require.NoError(t, os.WriteFile(path, []byte(tc.body), 0o600))
			}
			clock := &writingClock{testClock: testClock{now: time.Unix(1_800_000_000, 0)}, path: path, body: tc.body, nth: tc.nth}
			began := clock.now

			err := awaitTechnicians(path, techniciansWait, clock)

			if tc.refusal == "" {
				require.NoError(t, err)
				assert.Equal(t, tc.nth, clock.sleeps, "the walk starts at the first look that finds the file")
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.refusal)
			if tc.nth < 0 {
				assert.GreaterOrEqual(t, clock.now.Sub(began), techniciansWait, "the run gives the step its whole allowance")
				assert.Less(t, clock.now.Sub(began), techniciansWait+2*techniciansPoll)
			}
		})
	}
}

func TestARunWithNoTechniciansStepWaitsForNothing(t *testing.T) {
	clock := &writingClock{testClock: testClock{now: time.Unix(1_800_000_000, 0)}, nth: -1}

	require.NoError(t, awaitTechnicians("", techniciansWait, clock))
	assert.Zero(t, clock.sleeps)
	assert.Empty(t, captureStdout(t, func() { announceTechnicians("") }), "nobody is told to sign in")
}

func TestARunThatWaitsForTechniciansSaysWhenTheyMaySignIn(t *testing.T) {
	said := captureStdout(t, func() { announceTechnicians(filepath.Join(t.TempDir(), "technicians.ready")) })

	assert.Equal(t, "Technicians may sign in\n", said, "scripts/loadtest-k6-alongside.sh reads this line")
}
