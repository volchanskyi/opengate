package wsman

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/volchanskyi/opengate/server/internal/mps"
)

// amtWSManPort is the default WSMAN HTTP port on AMT devices.
const amtWSManPort = 16992

// Client sends WSMAN requests over an APF channel to an AMT device.
type Client struct {
	conn   *mps.Conn
	auth   DigestAuth
	mu     sync.Mutex // one WSMAN request at a time per connection
	logger *slog.Logger
}

// NewClient creates a WSMAN client for the given MPS connection.
func NewClient(conn *mps.Conn, username, password string, logger *slog.Logger) *Client {
	return &Client{
		conn:   conn,
		auth:   DigestAuth{Username: username, Password: password},
		logger: logger,
	}
}

// Do sends a WSMAN request to the AMT device, handling Digest auth transparently.
// It opens a channel to port 16992, sends the HTTP request, reads the response,
// and returns the response body.
func (c *Client) Do(ctx context.Context, soapAction string, body []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ch, err := c.conn.OpenChannel("127.0.0.1", amtWSManPort)
	if err != nil {
		return nil, fmt.Errorf("wsman: open channel: %w", err)
	}
	defer c.closeChannel(ch)

	cc := NewChannelConn(func(data []byte) error {
		return mps.WriteChannelData(c.conn.NetConn(), ch.RemoteID, data)
	})
	ch.SetOnData(cc.Feed)
	defer cc.Close()

	// First attempt without auth.
	status, respBody, wwwAuth, err := c.doHTTP(cc, soapAction, body, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized && wwwAuth != "" {
		// Retry with Digest auth.
		authHeader, err := c.auth.Authorize("POST", "/wsman", wwwAuth)
		if err != nil {
			return nil, fmt.Errorf("wsman: digest auth: %w", err)
		}
		status, respBody, _, err = c.doHTTP(cc, soapAction, body, authHeader)
		if err != nil {
			return nil, err
		}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("wsman: HTTP %d", status)
	}
	return respBody, nil
}

// doHTTP sends one HTTP POST to /wsman and reads the response.
func (c *Client) doHTTP(cc *ChannelConn, soapAction string, body []byte, authHeader string) (int, []byte, string, error) {
	// Build HTTP request.
	var sb strings.Builder
	sb.WriteString("POST /wsman HTTP/1.1\r\n")
	sb.WriteString(fmt.Sprintf("Host: 127.0.0.1:%d\r\n", amtWSManPort))
	sb.WriteString("Content-Type: application/soap+xml;charset=UTF-8\r\n")
	sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	if authHeader != "" {
		sb.WriteString(fmt.Sprintf("Authorization: %s\r\n", authHeader))
	}
	sb.WriteString("\r\n")

	// Send headers + body.
	if _, err := cc.Write([]byte(sb.String())); err != nil {
		return 0, nil, "", fmt.Errorf("wsman: write request: %w", err)
	}
	if _, err := cc.Write(body); err != nil {
		return 0, nil, "", fmt.Errorf("wsman: write body: %w", err)
	}

	// Read HTTP response.
	resp, err := http.ReadResponse(bufio.NewReader(cc), nil)
	if err != nil {
		return 0, nil, "", fmt.Errorf("wsman: read response: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, "", fmt.Errorf("wsman: read body: %w", err)
	}

	wwwAuth := resp.Header.Get("WWW-Authenticate")
	return resp.StatusCode, respBody, wwwAuth, nil
}

// closeChannel sends a channel close and ignores errors (best effort).
func (c *Client) closeChannel(ch *mps.Channel) {
	mps.WriteChannelClose(c.conn.NetConn(), ch.RemoteID) //nolint:errcheck
}
