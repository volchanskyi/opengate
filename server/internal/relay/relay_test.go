package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// mockConn wraps a net.Conn to satisfy the Conn interface.
type mockConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func newMockConnPair(t *testing.T) (*mockConn, *mockConn) {
	t.Helper()
	a, b := net.Pipe()
	ca := &mockConn{Conn: a, closed: make(chan struct{})}
	cb := &mockConn{Conn: b, closed: make(chan struct{})}
	t.Cleanup(func() { ca.Close(); cb.Close() })
	return ca, cb
}

func (c *mockConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func (c *mockConn) IsClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

func TestNewRelay_InitialState(t *testing.T) {
	r := NewRelay()
	assert.Equal(t, 0, r.ActiveSessionCount())
}

func TestRelay_Register_BothSides(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)
	_ = agentLocal
	_ = browserLocal

	ctx := context.Background()
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))
}

func TestRelay_Register_DuplicateSide(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, conn1 := newMockConnPair(t)
	_, conn2 := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, conn1, SideAgent))
	err := r.Register(ctx, token, conn2, SideAgent)
	assert.True(t, errors.Is(err, ErrDuplicateSide))
}

func TestRelay_Pipe_CopiesData(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	// Wait for piping goroutines to start
	time.Sleep(10 * time.Millisecond)

	// Agent writes, browser reads
	msg := []byte("hello from agent")
	_, err := agentLocal.Write(msg)
	require.NoError(t, err)

	buf := make([]byte, len(msg))
	_, err = io.ReadFull(browserLocal, buf)
	require.NoError(t, err)
	assert.Equal(t, msg, buf)
}

func TestRelay_Pipe_Bidirectional(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	agentLocal, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	// Agent → Browser
	agentMsg := []byte("from agent")
	_, err := agentLocal.Write(agentMsg)
	require.NoError(t, err)

	buf := make([]byte, len(agentMsg))
	_, err = io.ReadFull(browserLocal, buf)
	require.NoError(t, err)
	assert.Equal(t, agentMsg, buf)

	// Browser → Agent
	browserMsg := []byte("from browser")
	_, err = browserLocal.Write(browserMsg)
	require.NoError(t, err)

	buf = make([]byte, len(browserMsg))
	_, err = io.ReadFull(agentLocal, buf)
	require.NoError(t, err)
	assert.Equal(t, browserMsg, buf)
}

func TestRelay_CloseOnOneSideDisconnect(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	browserLocal, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	// Close agent side
	agentRelay.Close()

	// Browser side should see a read error shortly
	browserLocal.Conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err := browserLocal.Read(buf)
	assert.Error(t, err)
}

func TestRelay_ActiveSessionCount_Lifecycle(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)

	assert.Equal(t, 0, r.ActiveSessionCount())

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	assert.Equal(t, 1, r.ActiveSessionCount())

	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	// Close both sides to end the session
	agentRelay.Close()

	// Wait for cleanup
	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}

func TestRelay_ContextCancellation(t *testing.T) {
	r := NewRelay()
	token := protocol.GenerateSessionToken()
	ctx, cancel := context.WithCancel(context.Background())

	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)

	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))
	require.NoError(t, r.Register(ctx, token, browserRelay, SideBrowser))

	time.Sleep(10 * time.Millisecond)

	cancel()

	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond)
}
