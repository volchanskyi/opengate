package signaling

import (
	"fmt"
	"sync"
	"time"
)

// Phase represents the current signaling state of a session.
type Phase int

const (
	// PhaseRelay is the default state — using relay only.
	PhaseRelay Phase = iota
	// PhaseOffered means a SwitchToWebRTC offer has been sent.
	PhaseOffered
	// PhaseAnswered means the SDP answer has been received.
	PhaseAnswered
	// PhaseICEGathering means ICE candidates are being exchanged.
	PhaseICEGathering
	// PhaseConnected means both sides sent SwitchAck.
	PhaseConnected
	// PhaseFailed means the upgrade timed out or failed.
	PhaseFailed
)

// String returns a human-readable name for the phase.
func (p Phase) String() string {
	switch p {
	case PhaseRelay:
		return "relay"
	case PhaseOffered:
		return "offered"
	case PhaseAnswered:
		return "answered"
	case PhaseICEGathering:
		return "ice-gathering"
	case PhaseConnected:
		return "connected"
	case PhaseFailed:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", int(p))
	}
}

// validTransitions defines which phase transitions are allowed.
var validTransitions = map[Phase][]Phase{
	PhaseRelay:        {PhaseOffered, PhaseFailed},
	PhaseOffered:      {PhaseAnswered, PhaseFailed},
	PhaseAnswered:     {PhaseICEGathering, PhaseFailed},
	PhaseICEGathering: {PhaseConnected, PhaseFailed},
	PhaseConnected:    {},
	PhaseFailed:       {},
}

// SessionState tracks the signaling state for a single session.
type SessionState struct {
	mu        sync.Mutex
	phase     Phase
	offerTime time.Time
	ackCount  int // need 2 SwitchAck (one from each side) to complete
}

// NewSessionState creates a SessionState in PhaseRelay.
func NewSessionState() *SessionState {
	return &SessionState{
		phase: PhaseRelay,
	}
}

// Phase returns the current signaling phase.
func (s *SessionState) Phase() Phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

// Transition attempts to move to the target phase.
// Returns an error if the transition is not allowed.
func (s *SessionState) Transition(target Phase) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allowed := validTransitions[s.phase]
	for _, p := range allowed {
		if p == target {
			s.phase = target
			if target == PhaseOffered {
				s.offerTime = time.Now()
			}
			return nil
		}
	}

	return fmt.Errorf("invalid transition from %s to %s", s.phase, target)
}

// RecordAck records a SwitchAck from one side.
// Returns true when both sides have acknowledged (ackCount reaches 2).
func (s *SessionState) RecordAck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ackCount++
	return s.ackCount >= 2
}

// OfferTime returns when the offer was sent.
func (s *SessionState) OfferTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.offerTime
}
