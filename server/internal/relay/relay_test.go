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

func TestNewRelay_InitialState(t *testing.T) {
	r := NewRelay(slog.Default())
	assert.Equal(t, 0, r.ActiveSessionCount())
}

func TestRelay_Register_BothSides(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()

	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)

	ctx := context.Background()
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))
}

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

func TestRelay_Pipe_CopiesData(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	// Agent writes, browser reads — message boundary preserved
	msg := []byte("hello from agent")
	require.NoError(t, agentLocal.WriteMessage(msg))

	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, data)
}

func TestRelay_Pipe_Bidirectional(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	// Agent → Browser
	agentMsg := []byte("from agent")
	require.NoError(t, agentLocal.WriteMessage(agentMsg))
	data, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, agentMsg, data)

	// Browser → Agent
	browserMsg := []byte("from browser")
	require.NoError(t, browserLocal.WriteMessage(browserMsg))
	data, err = agentLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, browserMsg, data)
}

func TestRelay_Pipe_LargeMessage(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	// 256 KB — larger than the old 32KB io.CopyBuffer
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
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	// Close agent relay side — relay should tear down both sides
	agentRelay.Close()

	// Browser local should see EOF because relay closed browserRelay
	_, err := browserLocal.ReadMessage()
	assert.Error(t, err)
}

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

func (h *captureHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler          { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler               { return h }
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

// TestRelay_CopyMessages_LogsExactCount pins the msgs_copied attribute on
// the read-error log. Without this, the INCREMENT_DECREMENT mutation on
// `count++` (relay.go:144) survives because count is observable only via logs.
func TestRelay_CopyMessages_LogsExactCount(t *testing.T) {
	cap := &captureHandler{}
	r := NewRelay(slog.New(cap))
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

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

func TestRelay_ConnectionClose(t *testing.T) {
	r := NewRelay(slog.Default())
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	agentRelay.Close()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}
