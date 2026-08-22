package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A machine is not a burst of connections. It connects, stays connected for
// days, sends on a cadence, occasionally drops and comes back with a backlog,
// and sometimes reconnects before the server noticed it left. A harness that
// only opens connections measures the accept path and nothing that happens
// afterwards — which is most of what the server does.
//
// The behaviour is a state machine so it can be tested without a network: what
// a machine does next, and when, is a decision the tests can step through
// directly, and dialling is left to the thin part around it.

var simStart = time.Date(2026, 8, 21, 2, 0, 0, 0, time.UTC)

func steadyBehaviour() Behaviour {
	return Behaviour{
		HeartbeatEvery: 30 * time.Second,
		TelemetryEvery: time.Minute,
		HoldFor:        10 * time.Minute,
	}
}

func TestAnAgentConnectsBeforeItDoesAnythingElse(t *testing.T) {
	agent := NewSimAgent(1, steadyBehaviour(), simStart)

	action := agent.Next(simStart)

	assert.Equal(t, ActionConnect, action.Kind)
	assert.Equal(t, simStart, action.At)
}

// The cadence is the point. A machine that sent everything at once would
// exercise a burst the server never receives.
func TestAnAgentHeartbeatsOnItsCadence(t *testing.T) {
	agent := NewSimAgent(1, steadyBehaviour(), simStart)
	require.Equal(t, ActionConnect, agent.Next(simStart).Kind)
	agent.Did(ActionConnect, simStart)
	require.Equal(t, ActionRegister, agent.Next(simStart).Kind)
	agent.Did(ActionRegister, simStart)

	next := agent.Next(simStart)
	assert.Equal(t, ActionHeartbeat, next.Kind)
	assert.Equal(t, simStart.Add(30*time.Second), next.At,
		"the first heartbeat is one cadence after registration, not immediately")

	agent.Did(ActionHeartbeat, next.At)
	assert.Equal(t, simStart.Add(time.Minute), agent.Next(next.At).At)
}

// Every machine heartbeating on the same instant is a shape no fleet has, and
// it is the one that makes a server look fine right up until it does not.
func TestCadenceIsJitteredPerAgentAndStaysDeterministic(t *testing.T) {
	behaviour := steadyBehaviour()
	behaviour.JitterFraction = 0.2

	offsets := map[time.Duration]int{}
	for id := 1; id <= 20; id++ {
		agent := NewSimAgent(uint64(id), behaviour, simStart)
		agent.Did(ActionConnect, simStart)
		agent.Did(ActionRegister, simStart)
		offsets[agent.Next(simStart).At.Sub(simStart)]++
	}
	assert.Greater(t, len(offsets), 5, "the fleet must not heartbeat in lockstep")

	for offset := range offsets {
		assert.GreaterOrEqual(t, offset, 24*time.Second)
		assert.LessOrEqual(t, offset, 36*time.Second)
	}

	first := NewSimAgent(7, behaviour, simStart)
	second := NewSimAgent(7, behaviour, simStart)
	first.Did(ActionConnect, simStart)
	first.Did(ActionRegister, simStart)
	second.Did(ActionConnect, simStart)
	second.Did(ActionRegister, simStart)
	assert.Equal(t, first.Next(simStart).At, second.Next(simStart).At,
		"the same agent id must jitter the same way, or a run is not reproducible")
}

// A machine that loses its link and comes back has a backlog, and how the
// server admits that backlog is one of the things a load run exists to test.
func TestAnAgentReconnectsAndBackfills(t *testing.T) {
	behaviour := steadyBehaviour()
	behaviour.ReconnectAfter = 2 * time.Minute
	behaviour.BackfillBatches = 3

	agent := NewSimAgent(1, behaviour, simStart)
	kinds := drive(t, agent, simStart, 6*time.Minute)

	assert.Contains(t, kinds, ActionDisconnect)
	assert.Contains(t, kinds, ActionBackfill)
	assert.Greater(t, countKind(kinds, ActionConnect), 1, "a reconnect is another connect")
}

// A machine whose old connection the server has not yet noticed is gone
// reconnects into a duplicate. Which one wins is behaviour a run must exercise
// rather than assume.
func TestADuplicateConnectionIsItsOwnBehaviour(t *testing.T) {
	behaviour := steadyBehaviour()
	behaviour.DuplicateConnection = true

	agent := NewSimAgent(1, behaviour, simStart)
	kinds := drive(t, agent, simStart, 3*time.Minute)

	assert.Contains(t, kinds, ActionDuplicateConnect)
}

// A purged device must stop being served. The simulator keeps sending, which is
// what proves the refusal rather than assuming the agent politely stopped.
func TestATombstonedAgentKeepsSendingSoTheRefusalIsProven(t *testing.T) {
	behaviour := steadyBehaviour()
	behaviour.Tombstoned = true

	agent := NewSimAgent(1, behaviour, simStart)
	kinds := drive(t, agent, simStart, 3*time.Minute)

	assert.Contains(t, kinds, ActionHeartbeat,
		"a tombstoned agent still sends; the server is what must refuse")
	assert.True(t, agent.ExpectsRejection(), "every write it makes is an expected rejection, never a fault")
}

// A stop is declared, so a connection the run closed on purpose is not counted
// as one the server dropped.
func TestAnAgentStopsGracefullyWhenItsHoldEnds(t *testing.T) {
	agent := NewSimAgent(1, steadyBehaviour(), simStart)

	end := simStart.Add(10 * time.Minute)
	final := agent.Next(end.Add(time.Second))

	assert.Equal(t, ActionStop, final.Kind)
	assert.True(t, agent.Done(end.Add(time.Second)))
}

// A slow responder is what a machine on a congested link looks like, and it is
// the case that holds a server-side slot open.
func TestASlowAgentDelaysItsResponses(t *testing.T) {
	behaviour := steadyBehaviour()
	behaviour.ResponseDelay = 5 * time.Second

	agent := NewSimAgent(1, behaviour, simStart)
	assert.Equal(t, 5*time.Second, agent.ResponseDelay())
}

// A ramp is what keeps the first connection of a run from being counted as a
// regression: everything arriving at once measures a cold pool.
func TestARampArrivesGraduallyAndEndsAtTheFullFleet(t *testing.T) {
	ramp := Ramp{Total: 500, Over: time.Minute}

	assert.Equal(t, 0, ramp.ConnectedAt(0))
	assert.Equal(t, 250, ramp.ConnectedAt(30*time.Second))
	assert.Equal(t, 500, ramp.ConnectedAt(time.Minute))
	assert.Equal(t, 500, ramp.ConnectedAt(2*time.Minute), "a ramp holds at the full fleet, it does not overshoot")
}

// A step change has no ramp, and the two are different events the system
// absorbs differently — so a zero ramp must mean "all at once", not "never".
func TestAStepChangeConnectsEverythingAtOnce(t *testing.T) {
	step := Ramp{Total: 2000, Over: 0}

	assert.Equal(t, 2000, step.ConnectedAt(0))
}

func TestARampNeverReportsMoreThanTheFleet(t *testing.T) {
	ramp := Ramp{Total: 3, Over: time.Second}

	for _, elapsed := range []time.Duration{0, 100 * time.Millisecond, time.Second, time.Hour} {
		got := ramp.ConnectedAt(elapsed)
		assert.GreaterOrEqual(t, got, 0)
		assert.LessOrEqual(t, got, 3)
	}
}

// drive steps an agent through a window and returns what it did, so a
// behaviour's whole arc is one assertion rather than a sequence of them.
func drive(t *testing.T, agent *SimAgent, from time.Time, window time.Duration) []ActionKind {
	t.Helper()

	var kinds []ActionKind
	now := from
	deadline := from.Add(window)
	for range 500 {
		if agent.Done(now) {
			break
		}
		action := agent.Next(now)
		if action.At.After(deadline) {
			break
		}
		if action.At.After(now) {
			now = action.At
		}
		kinds = append(kinds, action.Kind)
		agent.Did(action.Kind, now)
	}
	require.NotEmpty(t, kinds)
	return kinds
}

func countKind(kinds []ActionKind, want ActionKind) int {
	n := 0
	for _, kind := range kinds {
		if kind == want {
			n++
		}
	}
	return n
}
