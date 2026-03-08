package signaling

import (
	"sync"
	"sync/atomic"
)

// Tracker manages signaling state across all active sessions.
type Tracker struct {
	sessions sync.Map // map[string]*SessionState (keyed by session token)
	config   Config

	successCount atomic.Int64
	failureCount atomic.Int64
}

// NewTracker creates a Tracker with the given config.
func NewTracker(cfg Config) *Tracker {
	return &Tracker{
		config: cfg,
	}
}

// StartSignaling creates and stores signaling state for a session.
// Returns the new SessionState.
func (t *Tracker) StartSignaling(token string) *SessionState {
	state := NewSessionState()
	t.sessions.Store(token, state)
	return state
}

// GetState returns the signaling state for a session, or nil if not found.
func (t *Tracker) GetState(token string) *SessionState {
	v, ok := t.sessions.Load(token)
	if !ok {
		return nil
	}
	return v.(*SessionState)
}

// RecordAck records a SwitchAck for a session.
// Returns true if both sides have acknowledged (upgrade complete).
func (t *Tracker) RecordAck(token string) bool {
	state := t.GetState(token)
	if state == nil {
		return false
	}
	complete := state.RecordAck()
	if complete {
		_ = state.Transition(PhaseConnected)
		t.successCount.Add(1)
	}
	return complete
}

// RecordFailure marks a session's signaling as failed and increments
// the failure counter.
func (t *Tracker) RecordFailure(token string) {
	state := t.GetState(token)
	if state == nil {
		return
	}
	_ = state.Transition(PhaseFailed)
	t.failureCount.Add(1)
}

// Remove deletes signaling state for a session.
func (t *Tracker) Remove(token string) {
	t.sessions.Delete(token)
}

// Config returns the tracker's signaling configuration.
func (t *Tracker) Config() Config {
	return t.config
}

// SuccessCount returns the number of successful WebRTC upgrades.
func (t *Tracker) SuccessCount() int64 {
	return t.successCount.Load()
}

// FailureCount returns the number of failed WebRTC upgrades.
func (t *Tracker) FailureCount() int64 {
	return t.failureCount.Load()
}
