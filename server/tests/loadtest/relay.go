package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"nhooyr.io/websocket"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// A relay session has two ends and neither one measures it alone. The operator's
// browser opens one; the machine opens the other; the server pipes between
// them. A scenario that opens only one end has nothing to time, which is how a
// relay latency metric came to be filled from an unauthenticated health check —
// the request went nowhere near the relay, and the three ceilings named after
// it could never fire.
//
// This is the machine's end. The load generator holds the browser's end and
// times its own frame coming back, so what it measures is the whole path.

// RelayRequest is where to join and which session to join.
type RelayRequest struct {
	// BaseURL is the server, as an HTTP origin. The scheme is switched to
	// WebSocket when dialling.
	BaseURL string
	Token   string
}

// RelayJoin is one held-open machine side of a relay session.
type RelayJoin struct {
	conn  *websocket.Conn
	token string
}

// Close ends the machine's side of the session deliberately, so a connection
// the run closed is not counted as one the server dropped.
func (j *RelayJoin) Close() error {
	if j == nil || j.conn == nil {
		return nil
	}
	return j.conn.Close(websocket.StatusNormalClosure, "load run finished")
}

// Token is the session this join belongs to.
func (j *RelayJoin) Token() string { return j.token }

// RelayRequestFrom reads a session request the server sent over the control
// stream. The relay URL names the server, so the origin is derived from it
// rather than configured separately — one address cannot then disagree with
// the other.
//
// The address is put through the same allowlist a configured target is. A relay
// URL arrives on the wire, and a field on the wire must not be able to send the
// generator somewhere the run is forbidden to go.
func RelayRequestFrom(msg *protocol.ControlMessage) (RelayRequest, error) {
	if msg == nil || msg.Type != protocol.MsgSessionRequest {
		return RelayRequest{}, fmt.Errorf("not a session request")
	}
	if msg.Token == "" {
		return RelayRequest{}, errors.New("session request names no token")
	}
	if msg.RelayURL == "" {
		return RelayRequest{}, errors.New("session request names no relay URL")
	}

	parsed, err := url.Parse(msg.RelayURL)
	if err != nil {
		return RelayRequest{}, fmt.Errorf("session request relay URL %q is not a URL: %w", msg.RelayURL, err)
	}

	base := url.URL{Scheme: httpSchemeFor(parsed.Scheme), Host: parsed.Host}
	if err := CheckTarget(base.String()); err != nil {
		return RelayRequest{}, err
	}

	return RelayRequest{BaseURL: base.String(), Token: string(msg.Token)}, nil
}

// JoinRelay opens the machine side of a session.
func JoinRelay(ctx context.Context, req RelayRequest) (*RelayJoin, error) {
	if req.Token == "" {
		return nil, errors.New("cannot join a relay session with no token")
	}
	if err := CheckTarget(req.BaseURL); err != nil {
		return nil, err
	}

	base, err := url.Parse(req.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("relay base URL %q is not a URL: %w", req.BaseURL, err)
	}
	dialURL := url.URL{
		Scheme:   wsSchemeFor(base.Scheme),
		Host:     base.Host,
		Path:     "/ws/relay/" + req.Token,
		RawQuery: "side=agent",
	}

	conn, _, err := websocket.Dial(ctx, dialURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("join relay session %s: %w", protocol.RedactToken(req.Token), err)
	}
	return &RelayJoin{conn: conn, token: req.Token}, nil
}

// Echo returns every frame it receives, which is what makes the round trip
// measurable from the browser side: the generator times its own frame coming
// back, so the number is the whole relay path rather than an unrelated request.
//
// It returns when ctx is cancelled or the peer goes away. Neither is a fault:
// the run ending and the operator closing the tab are both ordinary.
func (j *RelayJoin) Echo(ctx context.Context) error {
	for {
		kind, payload, err := j.conn.Read(ctx)
		if err != nil {
			return echoEnd(ctx, err)
		}
		if err := j.conn.Write(ctx, kind, payload); err != nil {
			return echoEnd(ctx, err)
		}
	}
}

// echoEnd separates an ordinary end from a real failure.
func echoEnd(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return nil
	}
	if websocket.CloseStatus(err) != -1 {
		return nil
	}
	return err
}

// httpSchemeFor maps a relay URL's scheme back to the HTTP origin it implies.
func httpSchemeFor(scheme string) string {
	if strings.EqualFold(scheme, "wss") || strings.EqualFold(scheme, "https") {
		return "https"
	}
	return "http"
}

// wsSchemeFor maps an HTTP origin's scheme to the WebSocket one.
func wsSchemeFor(scheme string) string {
	if strings.EqualFold(scheme, "https") || strings.EqualFold(scheme, "wss") {
		return "wss"
	}
	return "ws"
}
