package relay

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

func TestRelay_EndClosesBothSidesOfAPipingSessionWithTheReason(t *testing.T) {
	r := NewRelay(slog.Default())
	var ended atomic.Int32
	r.OnSessionEnd = func(protocol.SessionToken) { ended.Add(1) }

	token := protocol.GenerateSessionToken()
	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)
	agentDone := mustRegister(t, r, context.Background(), token, agentRelay, SideAgent)
	browserDone := mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	awaitPumping(t, agentLocal, browserLocal)

	require.True(t, r.End(token, "Ended by Carol"), "a live session is found and ended")

	for _, done := range []<-chan struct{}{agentDone, browserDone} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("a parked side was never released after the session was ended")
		}
	}
	assert.Equal(t, []string{"Ended by Carol"}, browserRelay.endReasons(),
		"the browser hears why its session ended")
	assert.Equal(t, []string{"Ended by Carol"}, agentRelay.endReasons())
	assert.Equal(t, int32(1), ended.Load(), "the row behind the session is cleaned up once")
	assert.Equal(t, 0, r.ActiveSessionCount())
	assert.False(t, r.End(token, "again"), "an ended session cannot be ended twice")
}

func TestRelay_EndReleasesASessionStillWaitingForItsPeer(t *testing.T) {
	r := NewRelay(slog.Default())
	var ended atomic.Int32
	r.OnSessionEnd = func(protocol.SessionToken) { ended.Add(1) }

	token := protocol.GenerateSessionToken()
	_, browserRelay := newMockConnPair(t)
	done := mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)

	require.True(t, r.End(token, "Opened elsewhere"))

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting side was never released")
	}
	assert.Equal(t, []string{"Opened elsewhere"}, browserRelay.endReasons())
	assert.Equal(t, int32(1), ended.Load())
	assert.Equal(t, 0, r.ActiveSessionCount())

	_, lateAgent := newMockConnPair(t)
	lateDone, err := r.Register(context.Background(), token, lateAgent, SideAgent)
	require.NoError(t, err, "a late side opens a fresh entry the peer-wait timeout reclaims")
	select {
	case <-lateDone:
		t.Fatal("a late side must not be paired with the ended session")
	default:
	}
	r.Unregister(token)
}

func TestRelay_EndOfAnUnknownSessionReportsNothingFound(t *testing.T) {
	r := NewRelay(slog.Default())
	assert.False(t, r.End(protocol.GenerateSessionToken(), "Ended by Carol"))
}

func TestRelay_ASideHangingUpClosesThePeerWithoutAReason(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)
	mustRegister(t, r, context.Background(), token, agentRelay, SideAgent)
	browserDone := mustRegister(t, r, context.Background(), token, browserRelay, SideBrowser)
	awaitPumping(t, agentLocal, browserLocal)

	require.NoError(t, agentLocal.Close())

	select {
	case <-browserDone:
	case <-time.After(2 * time.Second):
		t.Fatal("the browser side was never released")
	}
	assert.Empty(t, browserRelay.endReasons(), "a peer leaving is an ordinary close")
}
