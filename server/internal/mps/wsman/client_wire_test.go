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

	"github.com/volchanskyi/opengate/server/internal/mps"
)

// Phase B / B3: wire-level tests for wsman.Client.
//
// These tests exercise Do() and the three operation methods end-to-end through
// the production code paths (request encoding, channel data framing, digest
// auth retry, response parsing). The connection dependency is faked via the
// MPSConn interface: a *mps.Channel that we control and a net.Pipe() that an
// AMT-side goroutine reads/writes to.
//
// Bytes flow as in production:
//   wsman.Client.Do
//     -> opens our fake channel (RemoteID=42)
//     -> writes the HTTP request through mps.WriteChannelData(netConn, ...)
//     -> AMT goroutine reads APF ChannelData from the other end, parses HTTP
//     -> AMT goroutine delivers an HTTP response by calling ch.OnData(...)
//   wsman.Client.Do
//     -> parses the response from cc.Read (fed by OnData via the pipe)
//     -> returns

// fakeMPSConn implements MPSConn for tests.
type fakeMPSConn struct {
	netConn   net.Conn      // wsman client writes APF here
	ch        *mps.Channel  // returned by OpenChannel
	openErr   error         // optional: simulate OpenChannel failure
	openCalls int
}

func (f *fakeMPSConn) OpenChannel(_ string, _ uint16) (*mps.Channel, error) {
	f.openCalls++
	if f.openErr != nil {
		return nil, f.openErr
	}
	return f.ch, nil
}

func (f *fakeMPSConn) NetConn() net.Conn { return f.netConn }

// amtSimulator runs as a goroutine on the AMT side of the pipe. It reads APF
// ChannelData frames, accumulates the inner bytes into a *http.Request, and
// hands them to handler. handler returns the HTTP response bytes; the
// simulator delivers them by calling ch.OnData (mirroring what the production
// mps message loop does on incoming ChannelData).
type amtSimulator struct {
	t        *testing.T
	conn     net.Conn
	ch       *mps.Channel
	handler  func(req *http.Request) []byte
	done     chan struct{}
	requests int
}

func (a *amtSimulator) run() {
	defer close(a.done)
	for {
		req, err := readOneHTTPRequest(a.conn)
		if err != nil {
			return // pipe closed by client — test is done
		}
		a.requests++
		resp := a.handler(req)
		// Production Do() calls ch.SetOnData *before* writing the request
		// payload to netConn. The net.Pipe read on a.conn established a
		// happens-before edge with that write, so OnData is guaranteed
		// visible here without further synchronization on Channel.mu.
		if cb := a.ch.OnData; cb != nil {
			cb(resp)
		}
	}
}

// readOneHTTPRequest reads APF ChannelData messages off the wire and
// accumulates them until both the headers AND the full Content-Length body
// have arrived. Returning earlier than that would deadlock against the
// wsman client's two-step write (headers then body): the simulator would
// reply on cc.Feed before the second cc.Write completed, and both ends
// would block waiting on each other.
func readOneHTTPRequest(c net.Conn) (*http.Request, error) {
	var acc bytes.Buffer
	for {
		msgType, payload, err := mps.ReadMessage(c)
		if err != nil {
			return nil, err
		}
		if msgType != mps.APFChannelData {
			continue
		}
		cd, err := mps.ParseChannelData(payload)
		if err != nil {
			return nil, err
		}
		acc.Write(cd.Data)

		buf := acc.Bytes()
		headerEnd := bytes.Index(buf, []byte("\r\n\r\n"))
		if headerEnd < 0 {
			continue // headers not yet complete
		}
		bodyStart := headerEnd + 4
		r, parseErr := http.ReadRequest(bufio.NewReader(bytes.NewReader(buf)))
		if parseErr != nil {
			continue // headers not yet parseable (rare but defensive)
		}
		wantBody := max(0, int(r.ContentLength))
		if len(buf)-bodyStart < wantBody {
			continue // body bytes still arriving
		}
		// Replace the streaming body with a fixed-size reader the test
		// can fully drain without blocking.
		r.Body = io.NopCloser(bytes.NewReader(buf[bodyStart : bodyStart+wantBody]))
		return r, nil
	}
}

// newWireFixture sets up the fake MPSConn, fake Channel, and AMT simulator.
// handler is invoked once per HTTP request the client sends.
func newWireFixture(t *testing.T, handler func(req *http.Request) []byte) (*Client, *fakeMPSConn, func()) {
	t.Helper()
	clientSide, amtSide := net.Pipe()
	ch := &mps.Channel{LocalID: 1, RemoteID: 42, Type: "direct-tcpip"}

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

// httpOK builds an HTTP/1.1 200 OK response with the given body.
func httpOK(body string) []byte {
	return fmt.Appendf(nil,
		"HTTP/1.1 200 OK\r\nContent-Type: application/soap+xml\r\n"+
			"Content-Length: %d\r\n\r\n%s",
		len(body), body)
}

// httpUnauthorized builds a 401 response with a Digest challenge.
func httpUnauthorized(realm, nonce string) []byte {
	return fmt.Appendf(nil,
		"HTTP/1.1 401 Unauthorized\r\n"+
			"WWW-Authenticate: Digest realm=\"%s\", nonce=\"%s\", qop=\"auth\"\r\n"+
			"Content-Length: 0\r\n\r\n", realm, nonce)
}

// minimalSOAPEnvelope wraps body in a stub SOAP envelope for response parsing.
func minimalSOAPEnvelope(body string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">` +
		`<s:Body>` + body + `</s:Body></s:Envelope>`
}

// ---- Tests ----

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
	clientSide, amtSide := net.Pipe()
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
	// Server returns 401 with a non-Digest challenge — the auth retry must
	// fail at parseChallenge, surfaced as a wsman: digest auth error.
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
	// When the SOAP envelope is unparseable, GetDeviceInfo logs and returns
	// an empty (non-nil) DeviceInfo. This is intentional graceful handling.
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
		// SOAP envelope but with an empty body. ParseEnvelopeBody must fail.
		return httpOK(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"></s:Envelope>`)
	})
	defer cleanup()

	_, err := client.GetPowerState(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "empty soap body")
}
