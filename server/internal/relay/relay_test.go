package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// mockConn is a message-oriented in-memory connection for testing.
// Paired connections share a done channel so closing either end
// unblocks the other.
type mockConn struct {
	readCh  <-chan []byte
	writeCh chan<- []byte
	done    chan struct{} // shared between paired conns
	closeFn func()       // shared once-close of done
}

// newMockConnPair returns two connected mockConns: messages written to one
// are readable from the other, preserving message boundaries.
// Closing either end unblocks the other.
func newMockConnPair(t *testing.T) (*mockConn, *mockConn) {
	t.Helper()
	aToB := make(chan []byte, 16)
	bToA := make(chan []byte, 16)
	done := make(chan struct{})
	var once sync.Once
	closeFn := func() { once.Do(func() { close(done) }) }
	a := &mockConn{readCh: bToA, writeCh: aToB, done: done, closeFn: closeFn}
	b := &mockConn{readCh: aToB, writeCh: bToA, done: done, closeFn: closeFn}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

// ReadMessage returns the next message or io.EOF once the pair is closed.
func (c *mockConn) ReadMessage() ([]byte, error) {
	select {
	case data, ok := <-c.readCh:
		if !ok {
			return nil, io.EOF
		}
		return data, nil
	case <-c.done:
		return nil, io.EOF
	}
}

// WriteMessage copies and sends data, or returns io.ErrClosedPipe once closed.
func (c *mockConn) WriteMessage(data []byte) error {
	msg := make([]byte, len(data))
	copy(msg, data)
	select {
	case c.writeCh <- msg:
		return nil
	case <-c.done:
		return io.ErrClosedPipe
	}
}

// Close closes the shared done channel, unblocking both ends of the pair.
func (c *mockConn) Close() error {
	c.closeFn()
	return nil
}

// registerSession registers both sides of a fresh session on r and returns the
// token plus the local (test-controlled) ends of the agent and browser conns.
// Closing either local end tears the session down because paired mockConns
// share a done channel.
func registerSession(t *testing.T, r *Relay) (token protocol.SessionToken, agentLocal, browserLocal *mockConn) {
	t.Helper()
	token = protocol.GenerateSessionToken()
	ctx := context.Background()

	var agentRelay, browserRelay *mockConn
	agentLocal, agentRelay = newMockConnPair(t)
	browserLocal, browserRelay = newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))
	return token, agentLocal, browserLocal
}

// readyRelay is registerSession on a default-logger relay, returning the relay
// and the local ends for tests that don't need the token.
func readyRelay(t *testing.T) (r *Relay, agentLocal, browserLocal *mockConn) {
	t.Helper()
	r = NewRelay(slog.Default())
	_, agentLocal, browserLocal = registerSession(t, r)
	return r, agentLocal, browserLocal
}

// TestNewRelay_InitialState pins a fresh relay at zero active sessions.
func TestNewRelay_InitialState(t *testing.T) {
	r := NewRelay(slog.Default())
	assert.Equal(t, 0, r.ActiveSessionCount())
}

// TestRelay_Register_BothSides registers an agent and a browser on one token.
func TestRelay_Register_BothSides(t *testing.T) {
	readyRelay(t)
}

// TestRelay_Register_DuplicateSide rejects a second registration of one side.
func TestRelay_Register_DuplicateSide(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, conn1 := newMockConnPair(t)
	_, conn2 := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, conn1, SideAgent))
	err := r.Register(ctx, token, conn2, SideAgent)
	assert.True(t, errors.Is(err, ErrDuplicateSide))
}

// TestRelay_Pipe_CopiesData forwards one message agent→browser, boundary intact.
func TestRelay_Pipe_CopiesData(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	msg := []byte("hello from agent")
	require.NoError(t, agentLocal.WriteMessage(msg))

	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, data)
}

// TestRelay_Pipe_Bidirectional forwards messages in both directions.
func TestRelay_Pipe_Bidirectional(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	agentMsg := []byte("from agent")
	require.NoError(t, agentLocal.WriteMessage(agentMsg))
	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, agentMsg, data)

	browserMsg := []byte("from browser")
	require.NoError(t, browserLocal.WriteMessage(browserMsg))
	data, err = agentLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, browserMsg, data)
}

// TestRelay_Pipe_LargeMessage forwards a 256 KB payload (past the old 32 KB buf).
func TestRelay_Pipe_LargeMessage(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	largeMsg := make([]byte, 256*1024)
	for i := range largeMsg {
		largeMsg[i] = byte(i % 251)
	}
	require.NoError(t, agentLocal.WriteMessage(largeMsg))

	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, largeMsg, data)
}

// TestRelay_CloseOnOneSideDisconnect tears down both sides when one drops.
func TestRelay_CloseOnOneSideDisconnect(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	time.Sleep(10 * time.Millisecond)
	agentLocal.Close()

	// Browser local sees EOF because the relay closed its browser side.
	_, err := browserLocal.ReadMessage()
	assert.Error(t, err)
}

// TestRelay_ActiveSessionCount_Lifecycle tracks the count 0→1→0 across the
// first registration, the pairing, and teardown.
func TestRelay_ActiveSessionCount_Lifecycle(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)

	assert.Equal(t, 0, r.ActiveSessionCount())

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	assert.Equal(t, 1, r.ActiveSessionCount())

	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	agentRelay.Close()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

// TestRelay_Pipe_SurvivesRegisterContextCancel proves the pipe outlives the
// (per-request) registration context — frames flow after that ctx is cancelled.
func TestRelay_Pipe_SurvivesRegisterContextCancel(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	// Register agent with background context.
	require.NoError(t, r.Register(context.Background(), token, agentRelay, SideAgent))

	// Register browser with a cancellable context (simulates HTTP handler context).
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	// Verify data flows before cancellation.
	msg := []byte("before cancel")
	require.NoError(t, agentLocal.WriteMessage(msg))
	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, data)

	// Cancel the registration context — pipe must survive.
	cancel()
	time.Sleep(50 * time.Millisecond)

	// Data should still flow after context cancellation.
	msg2 := []byte("after cancel")
	require.NoError(t, agentLocal.WriteMessage(msg2))
	data2, err := browserLocal.ReadMessage()
	require.NoError(t, err, "pipe should survive registration context cancellation")
	assert.Equal(t, msg2, data2)

	assert.Equal(t, 1, r.ActiveSessionCount(), "session should still be active")
}

// captureHandler is a slog.Handler that records all attrs of every log record.
type captureHandler struct {
	mu      sync.Mutex
	records []map[string]any
}

// Enabled reports that every level is captured.
func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

// WithAttrs returns the same handler (test handler ignores grouping).
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

// WithGroup returns the same handler (test handler ignores grouping).
func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

// Handle records the message and every attribute of the log record.
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := map[string]any{"msg": r.Message}
	r.Attrs(func(a slog.Attr) bool {
		rec[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

// findFirst returns the first captured record whose "msg" equals msg, or nil.
func (h *captureHandler) findFirst(msg string) map[string]any {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r["msg"] == msg {
			return r
		}
	}
	return nil
}

// TestRelay_CopyMessages_LogsExactCount pins the msgs_copied attribute on
// the read-error log. Without this, the INCREMENT_DECREMENT mutation on
// `count++` (relay.go:144) survives because count is observable only via logs.
func TestRelay_CopyMessages_LogsExactCount(t *testing.T) {
	cap := &captureHandler{}
	r := NewRelay(slog.New(cap))
	_, agentLocal, browserLocal := registerSession(t, r)

	const n = 7
	for i := range n {
		require.NoError(t, agentLocal.WriteMessage([]byte{byte(i)}))
	}
	for range n {
		_, err := browserLocal.ReadMessage()
		require.NoError(t, err)
	}

	// Trigger a read error on the agent→browser direction by closing the
	// agent's write side — the copyMessages loop logs msgs_copied=n.
	agentLocal.Close()

	require.Eventually(t, func() bool {
		return cap.findFirst("relay read error") != nil
	}, time.Second, 10*time.Millisecond, "expected read-error log emitted")

	rec := cap.findFirst("relay read error")
	require.NotNil(t, rec)
	got, ok := rec["msgs_copied"].(int64)
	if !ok {
		// slog stores ints as int64 via Value.Any(); cover both shapes.
		gotInt, isInt := rec["msgs_copied"].(int)
		require.True(t, isInt, "msgs_copied not an int (got %T)", rec["msgs_copied"])
		got = int64(gotInt)
	}
	assert.Equal(t, int64(n), got, "expected %d messages logged as msgs_copied", n)
}

// TestRelay_ConnectionClose drives the active count back to zero after a close.
func TestRelay_ConnectionClose(t *testing.T) {
	r, agentLocal, _ := readyRelay(t)

	time.Sleep(10 * time.Millisecond)
	agentLocal.Close()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

// collectEvents drains up to `want` events from ch, returning early if the
// timeout elapses first.
func collectEvents(t *testing.T, ch <-chan SessionEvent, want int, timeout time.Duration) []SessionEvent {
	t.Helper()
	var got []SessionEvent
	deadline := time.After(timeout)
	for len(got) < want {
		select {
		case evt := <-ch:
			got = append(got, evt)
		case <-deadline:
			return got
		}
	}
	return got
}

// eventKinds projects a slice of SessionEvents down to their kinds.
func eventKinds(events []SessionEvent) []EventKind {
	kinds := make([]EventKind, 0, len(events))
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}

// testServerID is the fixed serverID used by registry-backed relay tests.
const testServerID = "server-A"

// registryRelay returns a relay wired to a fresh in-process registry under
// testServerID, along with that registry for ownership/event assertions.
func registryRelay(t *testing.T) (*Relay, *InProcessRegistry) {
	t.Helper()
	reg := NewInProcessRegistry()
	return NewRelay(slog.Default(), WithRegistry(reg, testServerID)), reg
}

// TestRelay_Register_ClaimsAffinityInRegistry asserts the first side of a
// session claims affinity for the relay's serverID via the SessionRegistry
// port (PR-A wiring, ADR-023). LookupOwner is the cross-server primitive the
// Phase 13b proxy will consume.
func TestRelay_Register_ClaimsAffinityInRegistry(t *testing.T) {
	r, reg := registryRelay(t)
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))

	owner, err := reg.LookupOwner(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, testServerID, owner)
}

// TestRelay_DefaultRegistry_TracksOwnership asserts NewRelay without options
// still wires an in-process registry (default serverID), so the live path is
// always registry-backed.
func TestRelay_DefaultRegistry_TracksOwnership(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))

	owner, err := r.registry.LookupOwner(ctx, token)
	require.NoError(t, err)
	assert.NotEmpty(t, owner)
}

// TestRelay_SessionEnd_DeletesRegistryEntry asserts teardown releases the
// affinity claim so a token does not leak ownership after both sides drop.
func TestRelay_SessionEnd_DeletesRegistryEntry(t *testing.T) {
	r, reg := registryRelay(t)
	ctx := context.Background()
	token, agentLocal, _ := registerSession(t, r)

	owner, err := reg.LookupOwner(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, testServerID, owner)

	agentLocal.Close()

	require.Eventually(t, func() bool {
		_, err := reg.LookupOwner(ctx, token)
		return errors.Is(err, ErrRegistryNotFound)
	}, time.Second, 10*time.Millisecond, "registry entry should be deleted on session end")
}

// TestRelay_Register_PublishesLifecycleEvents asserts the relay broadcasts
// EventCreated + EventSideJoined on registration and EventEnded on teardown,
// so peer servers (Phase 13b) can observe sessions they do not own.
func TestRelay_Register_PublishesLifecycleEvents(t *testing.T) {
	r, reg := registryRelay(t)

	// Subscribe before registering so the EventCreated broadcast is observed.
	events, err := reg.SubscribeEvents(t.Context())
	require.NoError(t, err)

	_, agentLocal, _ := registerSession(t, r)

	// First side: EventCreated + EventSideJoined; second side: EventSideJoined.
	startup := collectEvents(t, events, 3, time.Second)
	kinds := eventKinds(startup)
	assert.Contains(t, kinds, EventCreated)
	assert.Contains(t, kinds, EventSideJoined)

	agentLocal.Close()

	ended := collectEvents(t, events, 1, time.Second)
	require.Len(t, ended, 1)
	assert.Equal(t, EventEnded, ended[0].Kind)
}
