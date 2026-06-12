package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
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
	closeFn func()        // shared once-close of done
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

// testServerID is the fixed serverID used by registry-backed relay tests.
const testServerID = "server-A"

// stubRegistry is a configurable SessionRegistry for exercising the relay's
// error-handling branches — registry failures must be logged, never fatal.
type stubRegistry struct {
	saveErr   error
	deleteErr error
	pingErr   error
}

func (s *stubRegistry) SaveSession(context.Context, protocol.SessionToken, SessionMeta) error {
	return s.saveErr
}
func (s *stubRegistry) DeleteSession(context.Context, protocol.SessionToken) error {
	return s.deleteErr
}
func (s *stubRegistry) Ping(context.Context) error { return s.pingErr }

// TestRelay_RegistryErrors_AreNonFatal asserts that failures on the save and
// delete registry paths are logged but never break the live relay: data still
// flows and the session tears down cleanly.
func TestRelay_RegistryErrors_AreNonFatal(t *testing.T) {
	boom := errors.New("registry boom")
	reg := &stubRegistry{saveErr: boom, deleteErr: boom}
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID))
	_, agentLocal, browserLocal := registerSession(t, r)

	msg := []byte("still flows")
	require.NoError(t, agentLocal.WriteMessage(msg))
	got, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, got)

	agentLocal.Close()
	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

// TestRelay_PingRegistry surfaces the registry's health through the relay: nil
// when the backing store is reachable, the registry's error when it is not (the
// readiness probe drains the pod on that error).
func TestRelay_PingRegistry(t *testing.T) {
	healthy := NewRelay(slog.Default())
	require.NoError(t, healthy.PingRegistry(context.Background()))

	down := errors.New("registry unreachable")
	degraded := NewRelay(slog.Default(), WithRegistry(&stubRegistry{pingErr: down}, testServerID))
	require.ErrorIs(t, degraded.PingRegistry(context.Background()), down)
}

// pingToggleRegistry is an in-process registry whose Ping health can be flipped
// atomically from a test goroutine while the health monitor reads it — a plain
// mutable field would race the monitor. It embeds InProcessRegistry for the
// other five methods.
type pingToggleRegistry struct {
	*InProcessRegistry
	healthy atomic.Bool
}

func (p *pingToggleRegistry) Ping(context.Context) error {
	if p.healthy.Load() {
		return nil
	}
	return errors.New("registry unreachable")
}

// TestRelay_RegistryHealth_DegradedAfterThreshold pins the degraded-mode state
// machine: a first failure flips RegistryUp to false
// immediately, but the relay only enters degraded mode once the outage exceeds
// the threshold; the window is measured from the first failure; recovery clears
// both. Driven with explicit timestamps so it is fully deterministic.
func TestRelay_RegistryHealth_DegradedAfterThreshold(t *testing.T) {
	r := NewRelay(slog.Default(), WithRegistry(&stubRegistry{}, testServerID))
	t0 := time.Now()

	// Healthy by default — before any probe.
	assert.True(t, r.RegistryUp())
	assert.False(t, r.registryDegraded(t0))

	// First failure: down now, but not yet degraded.
	r.observeRegistryHealth(false, t0)
	assert.False(t, r.RegistryUp())
	assert.False(t, r.registryDegraded(t0))
	// Boundary is exclusive (>, not >=): exactly at the threshold is not degraded.
	assert.False(t, r.registryDegraded(t0.Add(DefaultDegradedThreshold)))
	assert.True(t, r.registryDegraded(t0.Add(DefaultDegradedThreshold+time.Nanosecond)))

	// A later failure keeps the original first-failure time — the window does not
	// reset on every probe.
	r.observeRegistryHealth(false, t0.Add(10*time.Second))
	assert.True(t, r.registryDegraded(t0.Add(DefaultDegradedThreshold+time.Nanosecond)))

	// Recovery clears both immediately.
	r.observeRegistryHealth(true, t0.Add(40*time.Second))
	assert.True(t, r.RegistryUp())
	assert.False(t, r.registryDegraded(t0.Add(100*time.Second)))
}

// TestRelay_Register_RefusesWhenDegraded asserts a brand-new session is refused
// with ErrRegistryDegraded once the registry outage exceeds the threshold, and
// no active-session slot is taken. WithDegradedThreshold(0) + a backdated failure
// makes the gate trip deterministically without a sleep.
func TestRelay_Register_RefusesWhenDegraded(t *testing.T) {
	r := NewRelay(slog.Default(), WithRegistry(&stubRegistry{}, testServerID), WithDegradedThreshold(0))
	r.observeRegistryHealth(false, time.Now().Add(-time.Second))
	require.True(t, r.RegistryDegraded())

	_, conn := newMockConnPair(t)
	err := r.Register(context.Background(), protocol.GenerateSessionToken(), conn, SideAgent)
	require.ErrorIs(t, err, ErrRegistryDegraded)
	assert.Equal(t, 0, r.ActiveSessionCount())
}

// TestRelay_Register_DegradedAllowsInFlightSecondSide asserts degraded mode only
// refuses *new* sessions: an in-flight session (first side already registered)
// still pairs its second side so existing traffic is never interrupted.
func TestRelay_Register_DegradedAllowsInFlightSecondSide(t *testing.T) {
	r := NewRelay(slog.Default(), WithRegistry(&stubRegistry{}, testServerID), WithDegradedThreshold(0))
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	// First side registers while the registry is healthy.
	_, agentRelay := newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))

	// Registry goes degraded.
	r.observeRegistryHealth(false, time.Now().Add(-time.Second))
	require.True(t, r.RegistryDegraded())

	// A brand-new session is refused...
	_, other := newMockConnPair(t)
	require.ErrorIs(t, r.Register(ctx, protocol.GenerateSessionToken(), other, SideAgent), ErrRegistryDegraded)

	// ...but the in-flight session's second side still pairs and pipes.
	_, browserRelay := newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))
	require.NoError(t, r.WaitForPeer(ctx, token))
}

// TestRelay_MonitorRegistryHealth_FlipsAndRecovers drives the background monitor
// against a toggleable registry: a failing probe flips RegistryUp to false, and
// once the registry recovers a later probe flips it back to true. No fixed sleep
// — require.Eventually waits on the monitor goroutine.
func TestRelay_MonitorRegistryHealth_FlipsAndRecovers(t *testing.T) {
	reg := &pingToggleRegistry{InProcessRegistry: NewInProcessRegistry()} // starts unhealthy
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID))
	require.True(t, r.RegistryUp()) // healthy until the first probe runs

	go r.MonitorRegistryHealth(t.Context(), 5*time.Millisecond)

	require.Eventually(t, func() bool { return !r.RegistryUp() }, time.Second, 5*time.Millisecond,
		"monitor should mark the registry down after a failed probe")

	reg.healthy.Store(true)
	require.Eventually(t, func() bool { return r.RegistryUp() }, time.Second, 5*time.Millisecond,
		"monitor should mark the registry back up after it recovers")
}
