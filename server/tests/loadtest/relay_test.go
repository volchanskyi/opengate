package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// The relay scenario used to fill its latency metric from an unauthenticated
// health check, so three ceilings sat on a number that never touched the relay.
// A real measurement needs both sides of the pipe: the operator's browser and
// the machine. The browser side belongs to the load generator; this is the
// machine side, held open by the harness so a session has somewhere to land.

// TestRelayJoinDialsTheAgentSideOfTheSessionItWasHandedProves the harness
// joins the session named in the request rather than a session of its own.
func TestRelayJoinDialsTheAgentSideOfTheSessionItWasHanded(t *testing.T) {
	var gotPath, gotSide string
	server := relayEchoServer(t, func(r *http.Request) {
		gotPath = r.URL.Path
		gotSide = r.URL.Query().Get("side")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	joined, err := JoinRelay(ctx, RelayRequest{
		BaseURL: server.URL,
		Token:   "abc123",
	})
	require.NoError(t, err)
	require.NoError(t, joined.Close())

	assert.Equal(t, "/ws/relay/abc123", gotPath)
	assert.Equal(t, "agent", gotSide, "the harness holds the machine side; the generator holds the browser side")
}

// A session request the server sends over QUIC is the only thing that tells the
// machine where to join. Reading it out of the control message is what pairs
// the two sides.
func TestRelayRequestIsReadFromTheSessionRequestFrame(t *testing.T) {
	msg := &protocol.ControlMessage{
		Type:     protocol.MsgSessionRequest,
		Token:    "tok",
		RelayURL: "ws://opengate-staging-server:8080/ws/relay/tok",
	}

	req, err := RelayRequestFrom(msg)
	require.NoError(t, err)
	assert.Equal(t, "tok", req.Token)
	assert.Equal(t, "http://opengate-staging-server:8080", req.BaseURL,
		"the relay URL names the server, so the base URL is derived rather than configured separately")
}

func TestRelayRequestRefusesAFrameThatNamesNoSession(t *testing.T) {
	for _, msg := range []*protocol.ControlMessage{
		{Type: protocol.MsgSessionRequest, RelayURL: "ws://server:8080/ws/relay/tok"},
		{Type: protocol.MsgSessionRequest, Token: "tok"},
		{Type: protocol.MsgAgentHeartbeat, Token: "tok", RelayURL: "ws://server:8080/ws/relay/tok"},
	} {
		_, err := RelayRequestFrom(msg)
		assert.Error(t, err)
	}
}

// The allowlist is not bypassable by a field on the wire. A relay URL arrives
// from the server, and a server the run should never be talking to would
// otherwise redirect the generator wherever it liked.
func TestRelayRequestObeysTheTargetAllowlist(t *testing.T) {
	_, err := RelayRequestFrom(&protocol.ControlMessage{
		Type:     protocol.MsgSessionRequest,
		Token:    "tok",
		RelayURL: "ws://opengate-server.opengate.svc.cluster.local:8080/ws/relay/tok",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

// Echoing is what makes the round trip measurable from the other side: the
// browser half times its own frame coming back, so what it measures is the
// whole relay path rather than an unrelated request.
func TestTheMachineSideEchoesWhatItIsSent(t *testing.T) {
	// The peer here stands in for the browser half: it sends one frame and
	// waits for the same bytes to come back.
	echoed := make(chan []byte, 1)
	peerErr := make(chan error, 1)
	server := relayPeerServer(t, func(ctx context.Context, conn *websocket.Conn) {
		if err := conn.Write(ctx, websocket.MessageBinary, []byte("ping")); err != nil {
			peerErr <- err
			return
		}
		_, payload, err := conn.Read(ctx)
		if err != nil {
			peerErr <- err
			return
		}
		echoed <- payload
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	joined, err := JoinRelay(ctx, RelayRequest{BaseURL: server.URL, Token: "abc123"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = joined.Close() })

	done := make(chan error, 1)
	go func() { done <- joined.Echo(ctx) }()

	select {
	case payload := <-echoed:
		assert.Equal(t, []byte("ping"), payload)
	case err := <-peerErr:
		t.Fatalf("peer never saw its frame come back: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("the machine side never echoed")
	}

	cancel()
	select {
	case err := <-done:
		assert.True(t, err == nil || errors.Is(err, context.Canceled) || websocket.CloseStatus(err) != -1,
			"a cancelled echo ends cleanly, got %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("echo did not stop when its context was cancelled")
	}
}

func TestJoinRelayRefusesADisallowedTarget(t *testing.T) {
	ctx := context.Background()

	_, err := JoinRelay(ctx, RelayRequest{
		BaseURL: "http://opengate-server.opengate.svc.cluster.local:8080",
		Token:   "abc123",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

func TestJoinRelayRefusesAnEmptyToken(t *testing.T) {
	_, err := JoinRelay(context.Background(), RelayRequest{BaseURL: "http://localhost:8080"})
	require.Error(t, err)
}

// relayEchoServer stands up a WebSocket endpoint shaped like the relay's, so
// the machine side is exercised without a cluster. inspect, when given, sees
// the upgrade request.
func relayEchoServer(t *testing.T, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	return relayServer(t, inspect, nil)
}

// relayPeerServer is the same endpoint with a peer on the far side, standing in
// for the browser half of a session.
func relayPeerServer(t *testing.T, peer func(context.Context, *websocket.Conn)) *httptest.Server {
	t.Helper()
	return relayServer(t, nil, peer)
}

func relayServer(t *testing.T, inspect func(*http.Request), peer func(context.Context, *websocket.Conn)) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inspect != nil {
			inspect(r)
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		if peer == nil {
			// Hold the connection until the joining side closes it, which is
			// what a held-open machine side looks like from the server.
			_, _, _ = conn.Read(r.Context())
			return
		}
		peer(r.Context(), conn)
	}))
	t.Cleanup(server.Close)
	return server
}
