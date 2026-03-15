package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/signaling"
)

func TestSignalingStartAndAck(t *testing.T) {
	tracker := signaling.NewTracker(signaling.DefaultConfig())

	token := string(protocol.GenerateSessionToken())

	// Start signaling
	tracker.StartSignaling(token)

	state := tracker.GetState(token)
	require.NotNil(t, state)
	assert.Equal(t, signaling.PhaseRelay, state.Phase())

	// Transition to offered
	require.NoError(t, state.Transition(signaling.PhaseOffered))
	assert.Equal(t, signaling.PhaseOffered, state.Phase())

	// Transition to answered
	require.NoError(t, state.Transition(signaling.PhaseAnswered))
	assert.Equal(t, signaling.PhaseAnswered, state.Phase())

	// Transition to ICE gathering
	require.NoError(t, state.Transition(signaling.PhaseICEGathering))

	// Record first ack (browser side)
	done := tracker.RecordAck(token)
	assert.False(t, done, "should not be done after 1 ack")

	// Record second ack (agent side)
	done = tracker.RecordAck(token)
	assert.True(t, done, "should be done after 2 acks")

	// State should be Connected
	assert.Equal(t, signaling.PhaseConnected, state.Phase())
	assert.Equal(t, int64(1), tracker.SuccessCount())
	assert.Equal(t, int64(0), tracker.FailureCount())
}

func TestSignalingRecordFailure(t *testing.T) {
	tracker := signaling.NewTracker(signaling.DefaultConfig())

	token := string(protocol.GenerateSessionToken())

	tracker.StartSignaling(token)

	state := tracker.GetState(token)
	require.NotNil(t, state)

	require.NoError(t, state.Transition(signaling.PhaseOffered))

	// Record failure
	tracker.RecordFailure(token)
	assert.Equal(t, signaling.PhaseFailed, state.Phase())
	assert.Equal(t, int64(0), tracker.SuccessCount())
	assert.Equal(t, int64(1), tracker.FailureCount())

	// Cannot transition after failure
	err := state.Transition(signaling.PhaseAnswered)
	assert.Error(t, err)
}

func TestSignalingConcurrentSessions(t *testing.T) {
	tracker := signaling.NewTracker(signaling.DefaultConfig())

	tokens := make([]string, 5)
	for i := range tokens {
		tokens[i] = string(protocol.GenerateSessionToken())
		tracker.StartSignaling(tokens[i])
	}

	// All sessions should be trackable independently
	for _, token := range tokens {
		state := tracker.GetState(token)
		require.NotNil(t, state, "session %s should exist", token[:8])
		assert.Equal(t, signaling.PhaseRelay, state.Phase())
	}

	// Advance first two to offered
	for _, token := range tokens[:2] {
		state := tracker.GetState(token)
		require.NoError(t, state.Transition(signaling.PhaseOffered))
	}

	// Fail the third
	tracker.RecordFailure(tokens[2])

	// Verify states are independent
	assert.Equal(t, signaling.PhaseOffered, tracker.GetState(tokens[0]).Phase())
	assert.Equal(t, signaling.PhaseOffered, tracker.GetState(tokens[1]).Phase())
	assert.Equal(t, signaling.PhaseFailed, tracker.GetState(tokens[2]).Phase())
	assert.Equal(t, signaling.PhaseRelay, tracker.GetState(tokens[3]).Phase())
	assert.Equal(t, signaling.PhaseRelay, tracker.GetState(tokens[4]).Phase())

	// Non-existent token
	assert.Nil(t, tracker.GetState("nonexistent"))

	// Remove one
	tracker.Remove(tokens[0])
	assert.Nil(t, tracker.GetState(tokens[0]))
}
