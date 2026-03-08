package signaling

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackerStartAndGet(t *testing.T) {
	tr := NewTracker(DefaultConfig())

	state := tr.StartSignaling("tok-1")
	require.NotNil(t, state)
	assert.Equal(t, PhaseRelay, state.Phase())

	got := tr.GetState("tok-1")
	assert.Equal(t, state, got)
}

func TestTrackerGetNonexistent(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	assert.Nil(t, tr.GetState("nope"))
}

func TestTrackerRemove(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	tr.StartSignaling("tok-1")

	tr.Remove("tok-1")
	assert.Nil(t, tr.GetState("tok-1"))
}

func TestTrackerRecordAck(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	state := tr.StartSignaling("tok-1")

	// Advance to ICEGathering so Connected is a valid transition
	require.NoError(t, state.Transition(PhaseOffered))
	require.NoError(t, state.Transition(PhaseAnswered))
	require.NoError(t, state.Transition(PhaseICEGathering))

	assert.False(t, tr.RecordAck("tok-1"))  // first ack
	assert.True(t, tr.RecordAck("tok-1"))   // second ack — complete
	assert.Equal(t, PhaseConnected, state.Phase())
	assert.Equal(t, int64(1), tr.SuccessCount())
}

func TestTrackerRecordAckNonexistent(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	assert.False(t, tr.RecordAck("nope"))
}

func TestTrackerRecordFailure(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	state := tr.StartSignaling("tok-1")
	require.NoError(t, state.Transition(PhaseOffered))

	tr.RecordFailure("tok-1")
	assert.Equal(t, PhaseFailed, state.Phase())
	assert.Equal(t, int64(1), tr.FailureCount())
}

func TestTrackerConfig(t *testing.T) {
	cfg := DefaultConfig()
	tr := NewTracker(cfg)
	assert.Equal(t, cfg.UpgradeTimeout, tr.Config().UpgradeTimeout)
}

func TestTrackerConcurrentAccess(t *testing.T) {
	tr := NewTracker(DefaultConfig())
	var wg sync.WaitGroup

	// Concurrent start/get/remove
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token := "tok"
			tr.StartSignaling(token)
			_ = tr.GetState(token)
			tr.Remove(token)
		}(i)
	}
	wg.Wait()
}
