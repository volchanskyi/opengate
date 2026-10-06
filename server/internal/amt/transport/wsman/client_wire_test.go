package wsman

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/amt/transport"
)

type fakeMPSConn struct {
	netConn   net.Conn
	ch        *transport.Channel
	openErr   error
	openCalls int
}

func (f *fakeMPSConn) OpenChannel(_ string, _ uint16) (*transport.Channel, error) {
	f.openCalls++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.ch, nil
}

func (f *fakeMPSConn) NetConn() net.Conn { return f.netConn }

type amtSimulator struct {
	t        *testing.T
	conn     net.Conn
	ch       *transport.Channel
	handler  func(req *http.Request) []byte
	done     chan struct{}
	requests int
}

func (a *amtSimulator) run() {
	defer close(a.done)
	for {
		req, err := readOneHTTPRequest(a.conn)
		if err != nil {
			return
		}
		a.requests++
		resp := a.handler(req)
		// Do sets OnData before writing the request, so the pipe read orders OnData visibly here.
		if cb := a.ch.OnData; cb != nil {
			cb(resp)
		}
	}
}

// The client writes headers then body, so the request is complete only once both have arrived;
// replying earlier deadlocks both ends.
func readOneHTTPRequest(c net.Conn) (*http.Request, error) {
	var acc bytes.Buffer
	for {
		msgType, payload, err := transport.ReadMessage(c)
		if err != nil {
			return nil, err
		}
		if msgType != transport.APFChannelData {
			continue
		}
		cd, err := transport.ParseChannelData(payload)
		if err != nil {
			return nil, err
		}
		acc.Write(cd.Data)

		buf := acc.Bytes()
		headerEnd := bytes.Index(buf, []byte("\r\n\r\n"))
		if headerEnd < 0 {
			continue
		}
		bodyStart := headerEnd + 4
		r, parseErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf)))
		if parseErr != nil {
			continue
		}
		wantBody := max(0, int(r.ContentLength))
		if len(buf)-bodyStart < wantBody {
			continue
		}
		r.Body = io.NopCloser(bytes.NewReader(buf[bodyStart : bodyStart+wantBody]))
		return r, nil
	}
}

const pipeTestTimeout = 5 * time.Second

// The pipe is unbuffered, so deadlines keep a silent simulator from blocking the client's Read.
func newDeadlinePipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	deadline := time.Now().Add(pipeTestTimeout)
	require.NoError(t, a.SetDeadline(deadline))
	require.NoError(t, b.SetDeadline(deadline))
	return a, b
}

func newWireFixture(t *testing.T, handler func(req *http.Request) []byte) (*Client, *fakeMPSConn, func()) {
	t.Helper()
	clientSide, amtSide := newDeadlinePipe(t)
	ch := &transport.Channel{LocalID: 1, RemoteID: 42, Type: "direct-tcpip"}

	fconn := &fakeMPSConn{netConn: clientSide, ch: ch}
	sim := &amtSimulator{
		t:       t,
		conn:    amtSide,
		ch:      ch,
		handler: handler,
		done:    make(chan struct{}),
	}
	go sim.run()

	client := NewClient(fconn, "admin", "P@ssw0rd",
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	cleanup := func() {
		_ = clientSide.Close()
		_ = amtSide.Close()
		select {
		case <-sim.done:
		case <-time.After(time.Second):
			t.Log("amt simulator did not exit cleanly")
		}
	}
	return client, fconn, cleanup
}

func httpOK(body string) []byte {
	return fmt.Appendf(nil,
		"HTTP/1.1 200 OK\r\nContent-Type: application/soap+xml\r\n"+
			"Content-Length: %d\r\n\r\n%s",
		len(body), body)
}

func httpUnauthorized(realm, nonce string) []byte {
	return fmt.Appendf(nil,
		"HTTP/1.1 401 Unauthorized\r\n"+
			"WWW-Authenticate: Digest realm=\"%s\", nonce=\"%s\", qop=\"auth\"\r\n"+
			"Content-Length: 0\r\n\r\n", realm, nonce)
}

func minimalSOAPEnvelope(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` +
		`<s:Body>` + body + `</s:Body></s:Envelope>`
}

func TestClientDo_HappyPathNoAuth(t *testing.T) {
	body := minimalSOAPEnvelope(`<r:Response>ok</r:Response>`)
	client, fconn, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(body)
	})
	defer cleanup()

	resp, err := client.Do(context.Background(), "TestAction", []byte("req-payload"))
	require.NoError(t, err)
	assert.Contains(t, string(resp), "ok")
	assert.Equal(t, 1, fconn.openCalls)
}

func TestClientDo_DigestRetrySucceeds(t *testing.T) {
	body := minimalSOAPEnvelope(`<r:Response>authok</r:Response>`)
	var seenAuth string
	var mu sync.Mutex
	calls := 0
	client, _, cleanup := newWireFixture(t, func(req *http.Request) []byte {
		mu.Lock()
		calls++
		c := calls
		mu.Unlock()
		if c == 1 {
			return httpUnauthorized("Digest:A4070000", "abc123")
		}
		mu.Lock()
		seenAuth = req.Header.Get("Authorization")
		mu.Unlock()
		return httpOK(body)
	})
	defer cleanup()

	resp, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.NoError(t, err)
	assert.Contains(t, string(resp), "authok")
	mu.Lock()
	assert.Contains(t, seenAuth, "Digest ")
	assert.Contains(t, seenAuth, `username="admin"`)
	assert.Contains(t, seenAuth, `nonce="abc123"`)
	mu.Unlock()
}

func TestClientDo_DigestRetryFinalNon200ReturnsError(t *testing.T) {
	calls := 0
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		calls++
		if calls == 1 {
			return httpUnauthorized("r", "n")
		}
		return []byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n")
	})
	defer cleanup()

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wsman: HTTP 500")
}

func TestClientDo_MalformedHTTPResponseFails(t *testing.T) {
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return []byte("not an HTTP response at all\r\n")
	})
	defer cleanup()

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read response")
}

func TestClientDo_OpenChannelFailurePropagates(t *testing.T) {
	clientSide, amtSide := newDeadlinePipe(t)
	defer clientSide.Close()
	defer amtSide.Close()

	fconn := &fakeMPSConn{
		netConn: clientSide,
		openErr: fmt.Errorf("simulated open failure"),
	}
	client := NewClient(fconn, "admin", "x",
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open channel")
	assert.Contains(t, err.Error(), "simulated open failure")
}

func TestClientDo_DigestChallengeRejected(t *testing.T) {
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return []byte("HTTP/1.1 401 Unauthorized\r\n" +
			"WWW-Authenticate: Basic realm=\"x\"\r\n" +
			"Content-Length: 0\r\n\r\n")
	})
	defer cleanup()

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest auth")
}

func TestRequestPowerStateChange_SendsExpectedAction(t *testing.T) {
	body := minimalSOAPEnvelope(`<r:OK/>`)
	var seenAction string
	var mu sync.Mutex
	client, _, cleanup := newWireFixture(t, func(req *http.Request) []byte {
		mu.Lock()
		seenAction = req.URL.Path
		mu.Unlock()
		return httpOK(body)
	})
	defer cleanup()

	err := client.RequestPowerStateChange(context.Background(), HardReset)
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "/wsman", seenAction)
}

func TestGetDeviceInfo_ParsesFields(t *testing.T) {
	respBody := minimalSOAPEnvelope(
		`<p:CIM_ComputerSystem>` +
			`<p:Name>amt-host-7</p:Name>` +
			`<p:Model>Optiplex 9020</p:Model>` +
			`</p:CIM_ComputerSystem>`)

	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})
	defer cleanup()

	info, err := client.GetDeviceInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "amt-host-7", info.Hostname)
	assert.Equal(t, "Optiplex 9020", info.Model)
}

func TestGetDeviceInfo_MalformedEnvelopeReturnsEmptyInfo(t *testing.T) {
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK("not xml at all <<<<")
	})
	defer cleanup()

	info, err := client.GetDeviceInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "", info.Hostname)
	assert.Equal(t, "", info.Model)
}

func TestGetPowerState_ParsesEnabledState(t *testing.T) {
	respBody := minimalSOAPEnvelope(`<p:EnabledState>2</p:EnabledState>`)
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})
	defer cleanup()

	state, err := client.GetPowerState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, PowerOn, state)
}

func TestGetPowerState_NonNumericEnabledStateReturnsError(t *testing.T) {
	respBody := minimalSOAPEnvelope(`<p:EnabledState>oops</p:EnabledState>`)
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})
	defer cleanup()

	_, err := client.GetPowerState(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse EnabledState")
}

func TestGetPowerState_EmptyBodyReturnsError(t *testing.T) {
	client, _, cleanup := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"></s:Envelope>`)
	})
	defer cleanup()

	_, err := client.GetPowerState(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "empty soap body")
}
