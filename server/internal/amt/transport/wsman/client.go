package wsman

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/volchanskyi/opengate/server/internal/amt/transport"
)

// amtWSManPort is the default WSMAN HTTP port on AMT devices.
const amtWSManPort = 16992

// Dial opens a byte stream to the device's WSMAN port; closing the stream ends it.
type Dial func(ctx context.Context) (io.ReadWriteCloser, error)

// OverMPS dials the device's WSMAN port through its CIRA connection.
func OverMPS(conn *transport.Conn) Dial {
	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		ch, err := conn.OpenChannel(ctx, "127.0.0.1", amtWSManPort)
		if err != nil {
			return nil, err
		}
		return ch, nil
	}
}

// Client sends WSMAN requests to an AMT device, each over a stream of its own, so requests on
// one client run side by side.
type Client struct {
	dial   Dial
	auth   DigestAuth
	logger *slog.Logger
}

// NewClient creates a WSMAN client that reaches the device through dial.
func NewClient(dial Dial, username, password string, logger *slog.Logger) *Client {
	return &Client{
		dial:   dial,
		auth:   DigestAuth{Username: username, Password: password},
		logger: logger,
	}
}

// Do sends a WSMAN request over a new stream, answers a Digest challenge, and returns the body.
// The stream closes when ctx ends, which ends a read the device never answers.
func (c *Client) Do(ctx context.Context, soapAction string, body []byte) ([]byte, error) {
	stream, err := c.dial(ctx)
	if err != nil {
		return nil, fmt.Errorf("wsman: open channel: %w", err)
	}
	stop := context.AfterFunc(ctx, func() { c.close(stream) })
	defer func() {
		stop()
		c.close(stream)
	}()

	respBody, err := c.exchange(stream, soapAction, body)
	if err != nil && ctx.Err() != nil {
		return nil, fmt.Errorf("wsman: %w", errors.Join(ctx.Err(), err))
	}
	return respBody, err
}

func (c *Client) exchange(stream io.ReadWriter, soapAction string, body []byte) ([]byte, error) {
	status, respBody, wwwAuth, err := c.doHTTP(stream, soapAction, body, "")
	if err != nil {
		return nil, err
	}
	if status == http.StatusUnauthorized && wwwAuth != "" {
		authHeader, err := c.auth.Authorize("POST", "/wsman", wwwAuth)
		if err != nil {
			return nil, fmt.Errorf("wsman: digest auth: %w", err)
		}
		status, respBody, _, err = c.doHTTP(stream, soapAction, body, authHeader)
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
func (c *Client) doHTTP(cc io.ReadWriter, soapAction string, body []byte, authHeader string) (int, []byte, string, error) {
	var sb strings.Builder
	sb.WriteString("POST /wsman HTTP/1.1\r\n")
	sb.WriteString(fmt.Sprintf("Host: 127.0.0.1:%d\r\n", amtWSManPort))
	sb.WriteString("Content-Type: application/soap+xml;charset=UTF-8\r\n")
	sb.WriteString(fmt.Sprintf("Content-Length: %d\r\n", len(body)))
	if authHeader != "" {
		sb.WriteString(fmt.Sprintf("Authorization: %s\r\n", authHeader))
	}
	sb.WriteString("\r\n")

	if _, err := cc.Write([]byte(sb.String())); err != nil {
		return 0, nil, "", fmt.Errorf("wsman: write request: %w", err)
	}
	if _, err := cc.Write(body); err != nil {
		return 0, nil, "", fmt.Errorf("wsman: write body: %w", err)
	}

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

// close ends the stream; a second close of a channel is a no-op.
func (c *Client) close(stream io.Closer) {
	if err := stream.Close(); err != nil {
		c.logger.Debug("wsman: close stream", "error", err)
	}
}
