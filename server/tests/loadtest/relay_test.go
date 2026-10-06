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

func TestRelayRequestObeysTheTargetAllowlist(t *testing.T) {
	_, err := RelayRequestFrom(&protocol.ControlMessage{
		Type:     protocol.MsgSessionRequest,
		Token:    "tok",
		RelayURL: "ws://opengate-server.opengate.svc.cluster.local:8080/ws/relay/tok",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed load-test target")
}

func TestTheMachineSideEchoesWhatItIsSent(t *testing.T) {
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

func relayEchoServer(t *testing.T, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	return relayServer(t, inspect, nil)
}

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
			_, _, _ = conn.Read(r.Context())
			return
		}
		peer(r.Context(), conn)
	}))
	t.Cleanup(server.Close)
	return server
}
