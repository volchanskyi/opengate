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

// mockConn is an in-memory Conn; paired mockConns share a done channel.
type mockConn struct {
	readCh  <-chan []byte
	writeCh chan<- []byte
	done    chan struct{}
	closeFn func()
}

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

func (c *mockConn) Close() error {
	c.closeFn()
	return nil
}

func mustRegister(t *testing.T, r *Relay, ctx context.Context, token protocol.SessionToken, conn Conn, side Side) <-chan struct{} {
	t.Helper()
	done, err := r.Register(ctx, token, conn, side)
	require.NoError(t, err)
	require.NotNil(t, done, "a registered side must be handed the channel its session ends on")
	return done
}

func registerSession(t *testing.T, r *Relay) (token protocol.SessionToken, agentLocal, browserLocal *mockConn) {
	t.Helper()
	token = protocol.GenerateSessionToken()
	ctx := context.Background()

	var agentRelay, browserRelay *mockConn
	agentLocal, agentRelay = newMockConnPair(t)
	browserLocal, browserRelay = newMockConnPair(t)
	mustRegister(t, r, ctx, token, agentRelay, SideAgent)
	mustRegister(t, r, ctx, token, browserRelay, SideBrowser)
	return token, agentLocal, browserLocal
}

func readyRelay(t *testing.T) (r *Relay, agentLocal, browserLocal *mockConn) {
	t.Helper()
	r = NewRelay(slog.Default())
	_, agentLocal, browserLocal = registerSession(t, r)
	return r, agentLocal, browserLocal
}

// awaitPumping round-trips a probe so both copy goroutines are running before a test closes a side.
func awaitPumping(t *testing.T, agentLocal, browserLocal *mockConn) {
	t.Helper()
	probe := []byte("pump-probe")
	require.NoError(t, agentLocal.WriteMessage(probe))
	got, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, probe, got)
}

func TestNewRelay_InitialState(t *testing.T) {
	r := NewRelay(slog.Default())
	assert.Equal(t, 0, r.ActiveSessionCount())
	assert.Equal(t, defaultServerID, r.serverID)
	assert.IsType(t, &InProcessRegistry{}, r.registry)
}

func TestRelay_Register_BothSides(t *testing.T) {
	readyRelay(t)
}

func TestRelay_Register_DuplicateSide(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, conn1 := newMockConnPair(t)
	_, conn2 := newMockConnPair(t)

	mustRegister(t, r, ctx, token, conn1, SideAgent)
	done, err := r.Register(ctx, token, conn2, SideAgent)
	assert.True(t, errors.Is(err, ErrDuplicateSide))
	assert.Nil(t, done, "a refused registration owns no session and must return no done channel")
}

func TestRelay_Pipe_CopiesData(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	msg := []byte("hello from agent")
	require.NoError(t, agentLocal.WriteMessage(msg))

	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, data)
}

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

func TestRelay_CloseOnOneSideDisconnect(t *testing.T) {
	_, agentLocal, browserLocal := readyRelay(t)

	awaitPumping(t, agentLocal, browserLocal)
	agentLocal.Close()

	_, err := browserLocal.ReadMessage()
	assert.Error(t, err)
}

func TestRelay_ActiveSessionCount_Lifecycle(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	assert.Equal(t, 0, r.ActiveSessionCount())

	mustRegister(t, r, ctx, token, agentRelay, SideAgent)
	assert.Equal(t, 1, r.ActiveSessionCount())

	mustRegister(t, r, ctx, token, browserRelay, SideBrowser)

	awaitPumping(t, agentLocal, browserLocal)

	agentRelay.Close()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

func TestRelay_SessionsStarted_CountsEachSessionOnce(t *testing.T) {
	r := NewRelay(slog.Default())
	ctx := context.Background()
	require.Zero(t, r.SessionsStarted())

	token := protocol.GenerateSessionToken()
	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	mustRegister(t, r, ctx, token, agentRelay, SideAgent)
	require.Equal(t, uint64(1), r.SessionsStarted(), "the first side starts the session")
	mustRegister(t, r, ctx, token, browserRelay, SideBrowser)
	require.Equal(t, uint64(1), r.SessionsStarted(), "the second side joins it rather than starting another")

	_, duplicate := newMockConnPair(t)
	_, err := r.Register(ctx, token, duplicate, SideBrowser)
	require.ErrorIs(t, err, ErrDuplicateSide)
	require.Equal(t, uint64(1), r.SessionsStarted(), "a refused registration starts nothing")

	awaitPumping(t, agentLocal, browserLocal)
	agentRelay.Close()
	require.Eventually(t, func() bool { return r.ActiveSessionCount() == 0 }, time.Second, 10*time.Millisecond)
	require.Equal(t, uint64(1), r.SessionsStarted(), "a session that ended still started")

	unpaired := protocol.GenerateSessionToken()
	_, waiting := newMockConnPair(t)
	mustRegister(t, r, ctx, unpaired, waiting, SideAgent)
	r.Unregister(unpaired)
	require.Equal(t, uint64(2), r.SessionsStarted(), "a session that never paired was open, so it started")
	require.Zero(t, r.ActiveSessionCount())
}

func TestRelay_ActiveTokens_ReportsLiveSessions(t *testing.T) {
	r := NewRelay(slog.Default())
	assert.Empty(t, r.ActiveTokens())

	token, agentLocal, _ := registerSession(t, r)
	assert.Equal(t, []protocol.SessionToken{token}, r.ActiveTokens())

	agentLocal.Close()
	require.Eventually(t, func() bool {
		return len(r.ActiveTokens()) == 0
	}, time.Second, 10*time.Millisecond)
}

func TestRelay_Pipe_SurvivesRegisterContextCancel(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	mustRegister(t, r, context.Background(), token, agentRelay, SideAgent)

	ctx, cancel := context.WithCancel(context.Background())
	mustRegister(t, r, ctx, token, browserRelay, SideBrowser)

	msg := []byte("before cancel")
	require.NoError(t, agentLocal.WriteMessage(msg))
	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, data)

	cancel()
	time.Sleep(50 * time.Millisecond)

	msg2 := []byte("after cancel")
	require.NoError(t, agentLocal.WriteMessage(msg2))
	data2, err := browserLocal.ReadMessage()
	require.NoError(t, err, "pipe should survive registration context cancellation")
	assert.Equal(t, msg2, data2)

	assert.Equal(t, 1, r.ActiveSessionCount(), "session should still be active")
}

// captureHandler is a slog.Handler that records every attribute of every record.
type captureHandler struct {
	mu      sync.Mutex
	records []map[string]any
}

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }

func (h *captureHandler) WithGroup(_ string) slog.Handler { return h }

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

func TestRelay_CopyMessages_LogsExactCount(t *testing.T) {
	logs := &captureHandler{}
	r := NewRelay(slog.New(logs))
	_, agentLocal, browserLocal := registerSession(t, r)

	const n = 7
	for i := range n {
		require.NoError(t, agentLocal.WriteMessage([]byte{byte(i)}))
	}
	for range n {
		_, err := browserLocal.ReadMessage()
		require.NoError(t, err)
	}

	agentLocal.Close()

	require.Eventually(t, func() bool {
		return logs.findFirst("relay read error") != nil
	}, time.Second, 10*time.Millisecond, "expected read-error log emitted")

	rec := logs.findFirst("relay read error")
	require.NotNil(t, rec)
	got, ok := rec["msgs_copied"].(int64)
	if !ok {
		// slog stores ints as int64 or int depending on the value's kind.
		gotInt, isInt := rec["msgs_copied"].(int)
		require.True(t, isInt, "msgs_copied not an int (got %T)", rec["msgs_copied"])
		got = int64(gotInt)
	}
	assert.Equal(t, int64(n), got, "expected %d messages logged as msgs_copied", n)
}

func TestRelay_ConnectionClose(t *testing.T) {
	r, agentLocal, browserLocal := readyRelay(t)

	awaitPumping(t, agentLocal, browserLocal)
	agentLocal.Close()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

const testServerID = "server-A"

// stubRegistry is a SessionRegistry whose methods return the configured errors.
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

func TestRelay_PingRegistry(t *testing.T) {
	healthy := NewRelay(slog.Default())
	require.NoError(t, healthy.PingRegistry(context.Background()))

	down := errors.New("registry unreachable")
	unhealthy := NewRelay(slog.Default(), WithRegistry(&stubRegistry{pingErr: down}, testServerID))
	require.ErrorIs(t, unhealthy.PingRegistry(context.Background()), down)
}
