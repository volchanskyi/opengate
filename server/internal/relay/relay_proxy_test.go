package relay

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// fakePeerDialer is a test PeerDialer. It records the dial arguments and, unless
// dialErr is set, returns relayEnd as the peer Conn — the test drives the owner
// side of the tunnel through the paired peerEnd.
type fakePeerDialer struct {
	mu       sync.Mutex
	calls    int
	gotOwner string
	gotToken protocol.SessionToken
	gotSide  Side
	dialErr  error
	relayEnd Conn // returned to the relay as the cross-server peer Conn
}

func (d *fakePeerDialer) Dial(_ context.Context, owner string, token protocol.SessionToken, side Side) (Conn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	d.gotOwner = owner
	d.gotToken = token
	d.gotSide = side
	if d.dialErr != nil {
		return nil, d.dialErr
	}
	return d.relayEnd, nil
}

func (d *fakePeerDialer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// proxiedRelay returns a relay whose registry reports a foreign owner (so the
// first registered side is proxied) wired to dialer, plus the peerEnd the test
// uses to act as the remote owner.
func proxiedRelay(t *testing.T, dialer *fakePeerDialer) (*Relay, *mockConn) {
	t.Helper()
	peerEnd, relayEnd := newMockConnPair(t)
	dialer.relayEnd = relayEnd
	reg := &stubRegistry{claimOwner: "owner-server"}
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID), WithPeerDialer(dialer))
	return r, peerEnd
}

// TestRelay_Proxy_SplicesBothDirections proves that when ClaimAffinity reports a
// foreign owner and a PeerDialer is set, the local conn is spliced to the owner
// tunnel in both directions instead of pairing locally.
func TestRelay_Proxy_SplicesBothDirections(t *testing.T) {
	dialer := &fakePeerDialer{}
	r, peerEnd := proxiedRelay(t, dialer)

	token := protocol.GenerateSessionToken()
	local, relayConn := newMockConnPair(t)
	require.NoError(t, r.Register(context.Background(), token, relayConn, SideAgent))

	// The dial carried the owner, token, and the proxied side.
	assert.Equal(t, 1, dialer.callCount())
	assert.Equal(t, "owner-server", dialer.gotOwner)
	assert.Equal(t, token, dialer.gotToken)
	assert.Equal(t, SideAgent, dialer.gotSide)
	assert.Equal(t, 1, r.ActiveSessionCount())

	// local → owner.
	require.NoError(t, local.WriteMessage([]byte("to owner")))
	got, err := peerEnd.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, []byte("to owner"), got)

	// owner → local.
	require.NoError(t, peerEnd.WriteMessage([]byte("from owner")))
	got, err = local.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, []byte("from owner"), got)
}

// TestRelay_Proxy_WaitForPeerUnblocks asserts the proxied session is marked ready
// once the tunnel is established, so the HTTP handler's WaitForPeer returns.
func TestRelay_Proxy_WaitForPeerUnblocks(t *testing.T) {
	dialer := &fakePeerDialer{}
	r, _ := proxiedRelay(t, dialer)

	token := protocol.GenerateSessionToken()
	_, relayConn := newMockConnPair(t)
	require.NoError(t, r.Register(context.Background(), token, relayConn, SideBrowser))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, r.WaitForPeer(ctx, token))
}

// TestRelay_Proxy_TeardownOnClose drains the proxied session when either end of
// the tunnel closes.
func TestRelay_Proxy_TeardownOnClose(t *testing.T) {
	dialer := &fakePeerDialer{}
	r, peerEnd := proxiedRelay(t, dialer)

	token := protocol.GenerateSessionToken()
	_, relayConn := newMockConnPair(t)
	require.NoError(t, r.Register(context.Background(), token, relayConn, SideAgent))
	require.Equal(t, 1, r.ActiveSessionCount())

	// Owner end drops → splice ends → session torn down.
	peerEnd.Close()
	require.Eventually(t, func() bool {
		return r.ActiveSessionCount() == 0
	}, time.Second, 10*time.Millisecond, "proxied session should tear down on tunnel close")
}

// TestRelay_Proxy_DialFailureClosesLocal asserts a dial failure fails fast: the
// local conn is closed, the session is removed, and Register returns an error so
// the client reconnects with a fresh token.
func TestRelay_Proxy_DialFailureClosesLocal(t *testing.T) {
	dialer := &fakePeerDialer{dialErr: errors.New("owner unreachable")}
	r, _ := proxiedRelay(t, dialer)

	token := protocol.GenerateSessionToken()
	local, relayConn := newMockConnPair(t)
	err := r.Register(context.Background(), token, relayConn, SideAgent)
	require.Error(t, err)

	// Local conn was closed by the failed splice.
	_, readErr := local.ReadMessage()
	assert.Error(t, readErr)
	assert.Equal(t, 0, r.ActiveSessionCount())
}

// TestRelay_Proxy_LocalOwnerZeroDial asserts that when this server owns the
// session (owner == serverID) the dialer is never called and the session pairs
// locally as usual — same-server sessions stay zero-hop.
func TestRelay_Proxy_LocalOwnerZeroDial(t *testing.T) {
	dialer := &fakePeerDialer{}
	// Default in-process registry: the caller always wins its own claim.
	reg := NewInProcessRegistry()
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID), WithPeerDialer(dialer))

	_, agentLocal, browserLocal := registerSession(t, r)

	msg := []byte("local hop")
	require.NoError(t, agentLocal.WriteMessage(msg))
	got, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, got)
	assert.Equal(t, 0, dialer.callCount(), "owner==self must not dial a peer")
}

// TestRelay_RegisterLocal_PairsWithLocalPeer asserts a proxied side delivered to
// the owner via RegisterLocal pairs with the already-local side and pipes both
// directions, without re-claiming affinity.
func TestRelay_RegisterLocal_PairsWithLocalPeer(t *testing.T) {
	reg := NewInProcessRegistry()
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID))
	token := protocol.GenerateSessionToken()
	ctx := context.Background()

	// Owner's local side arrives via the normal path (owns affinity).
	agentLocal, agentRelay := newMockConnPair(t)
	require.NoError(t, r.Register(ctx, token, agentRelay, SideAgent))

	// Proxied browser side arrives over the tunnel via RegisterLocal.
	browserLocal, browserRelay := newMockConnPair(t)
	require.NoError(t, r.RegisterLocal(ctx, token, browserRelay, SideBrowser))

	msg := []byte("agent→proxied browser")
	require.NoError(t, agentLocal.WriteMessage(msg))
	got, err := browserLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, msg, got)

	back := []byte("proxied browser→agent")
	require.NoError(t, browserLocal.WriteMessage(back))
	got, err = agentLocal.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, back, got)
}

// TestRelay_RegisterLocal_NeverProxies is the loop guard: RegisterLocal must
// never consult the PeerDialer, even when the registry reports a foreign owner.
// A proxied conn that re-proxied would loop forever between servers.
func TestRelay_RegisterLocal_NeverProxies(t *testing.T) {
	dialer := &fakePeerDialer{}
	r, _ := proxiedRelay(t, dialer) // registry reports a foreign owner

	token := protocol.GenerateSessionToken()
	// Both proxied sides arrive concurrently (each in its own handler), pair,
	// and complete — RegisterLocal blocks until its peer is present.
	_, agentRelay := newMockConnPair(t)
	_, browserRelay := newMockConnPair(t)
	errCh := make(chan error, 2)
	go func() { errCh <- r.RegisterLocal(context.Background(), token, agentRelay, SideAgent) }()
	go func() { errCh <- r.RegisterLocal(context.Background(), token, browserRelay, SideBrowser) }()
	require.NoError(t, <-errCh)
	require.NoError(t, <-errCh)

	assert.Equal(t, 0, dialer.callCount(), "RegisterLocal must never dial a peer")
}

// TestRelay_RegisterLocal_HalfOpenTimesOut asserts that when no local peer
// appears within affinityTTL (stale affinity: the owner-side conn already gone),
// RegisterLocal tears the half-open session down, closes the conn, and errors so
// the caller closes the proxied tunnel and the client reconnects.
func TestRelay_RegisterLocal_HalfOpenTimesOut(t *testing.T) {
	reg := NewInProcessRegistry()
	r := NewRelay(slog.Default(), WithRegistry(reg, testServerID), WithAffinityTTL(50*time.Millisecond))

	token := protocol.GenerateSessionToken()
	local, relayConn := newMockConnPair(t)
	err := r.RegisterLocal(context.Background(), token, relayConn, SideAgent)
	require.Error(t, err)

	_, readErr := local.ReadMessage()
	assert.Error(t, readErr, "half-open conn should be closed on timeout")
	assert.Equal(t, 0, r.ActiveSessionCount(), "half-open session should be torn down")
}

// TestRelay_Proxy_SecondSideRejected asserts that once a session is proxied, a
// second local side on the same server is rejected (it must reconnect and proxy
// independently) rather than corrupting the in-flight splice.
func TestRelay_Proxy_SecondSideRejected(t *testing.T) {
	dialer := &fakePeerDialer{}
	r, _ := proxiedRelay(t, dialer)

	token := protocol.GenerateSessionToken()
	_, firstConn := newMockConnPair(t)
	require.NoError(t, r.Register(context.Background(), token, firstConn, SideAgent))

	_, secondConn := newMockConnPair(t)
	err := r.Register(context.Background(), token, secondConn, SideBrowser)
	assert.True(t, errors.Is(err, ErrSessionProxied), "second side on a proxied session must be rejected")
}
