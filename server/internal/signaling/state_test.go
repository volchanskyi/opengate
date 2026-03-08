package signaling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSessionState(t *testing.T) {
	s := NewSessionState()
	assert.Equal(t, PhaseRelay, s.Phase())
}

func TestPhaseString(t *testing.T) {
	tests := []struct {
		phase Phase
		want  string
	}{
		{PhaseRelay, "relay"},
		{PhaseOffered, "offered"},
		{PhaseAnswered, "answered"},
		{PhaseICEGathering, "ice-gathering"},
		{PhaseConnected, "connected"},
		{PhaseFailed, "failed"},
		{Phase(99), "unknown(99)"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.phase.String())
		})
	}
}

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		name   string
		from   Phase
		to     Phase
		wantOk bool
	}{
		{"relay to offered", PhaseRelay, PhaseOffered, true},
		{"relay to failed", PhaseRelay, PhaseFailed, true},
		{"relay to connected", PhaseRelay, PhaseConnected, false},
		{"relay to answered", PhaseRelay, PhaseAnswered, false},
		{"offered to answered", PhaseOffered, PhaseAnswered, true},
		{"offered to failed", PhaseOffered, PhaseFailed, true},
		{"offered to connected", PhaseOffered, PhaseConnected, false},
		{"answered to ice-gathering", PhaseAnswered, PhaseICEGathering, true},
		{"answered to failed", PhaseAnswered, PhaseFailed, true},
		{"ice-gathering to connected", PhaseICEGathering, PhaseConnected, true},
		{"ice-gathering to failed", PhaseICEGathering, PhaseFailed, true},
		{"connected to anything", PhaseConnected, PhaseFailed, false},
		{"failed to anything", PhaseFailed, PhaseRelay, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewSessionState()
			// Advance to the 'from' phase
			advanceTo(t, s, tt.from)

			err := s.Transition(tt.to)
			if tt.wantOk {
				require.NoError(t, err)
				assert.Equal(t, tt.to, s.Phase())
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid transition")
			}
		})
	}
}

func TestTransitionSetsOfferTime(t *testing.T) {
	s := NewSessionState()
	assert.True(t, s.OfferTime().IsZero())

	require.NoError(t, s.Transition(PhaseOffered))
	assert.False(t, s.OfferTime().IsZero())
}

func TestRecordAck(t *testing.T) {
	s := NewSessionState()

	// First ack: not complete
	assert.False(t, s.RecordAck())

	// Second ack: complete
	assert.True(t, s.RecordAck())
}

// advanceTo moves a SessionState through valid transitions to reach the target phase.
func advanceTo(t *testing.T, s *SessionState, target Phase) {
	t.Helper()
	path := []Phase{PhaseRelay, PhaseOffered, PhaseAnswered, PhaseICEGathering, PhaseConnected}
	for _, p := range path {
		if s.Phase() == target {
			return
		}
		if p <= s.Phase() {
			continue
		}
		require.NoError(t, s.Transition(p))
	}
}
