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
)

// stream is the client's end of a pipe whose other end the simulator answers.
type stream struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (s *stream) Close() error {
	s.once.Do(func() { close(s.closed) })
	return s.Conn.Close()
}

// fakeDial hands out one stream per call and counts the calls.
type fakeDial struct {
	mu      sync.Mutex
	streams []*stream
	openErr error
	serve   func(amtSide net.Conn)
}

func (f *fakeDial) dial(_ context.Context) (io.ReadWriteCloser, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	clientSide, amtSide := net.Pipe()
	s := &stream{Conn: clientSide, closed: make(chan struct{})}
	f.mu.Lock()
	f.streams = append(f.streams, s)
	f.mu.Unlock()
	go f.serve(amtSide)
	return s, nil
}

func (f *fakeDial) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.streams)
}

// answer serves each HTTP request read from amtSide with handler's response.
func answer(handler func(req *http.Request) []byte) func(net.Conn) {
	return func(amtSide net.Conn) {
		defer amtSide.Close()
		reader := bufio.NewReader(amtSide)
		for {
			req, err := http.ReadRequest(reader)
			if err != nil {
				return
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			if _, err := amtSide.Write(handler(req)); err != nil {
				return
			}
		}
	}
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newWireFixture(t *testing.T, handler func(req *http.Request) []byte) (*Client, *fakeDial) {
	t.Helper()
	dial := &fakeDial{serve: answer(handler)}
	return NewClient(dial.dial, "admin", "P@ssw0rd", discard()), dial
}

const pipeTestTimeout = 5 * time.Second

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
	client, fconn := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(body)
	})

	resp, err := client.Do(context.Background(), "TestAction", []byte("req-payload"))
	require.NoError(t, err)
	assert.Contains(t, string(resp), "ok")
	require.Equal(t, 1, fconn.calls())
	select {
	case <-fconn.streams[0].closed:
	default:
		t.Fatal("Do left its stream open")
	}
}

func TestClientDo_HonoursItsContext(t *testing.T) {
	dial := &fakeDial{serve: func(amtSide net.Conn) {
		// The device reads the request and never answers.
		_, _ = io.Copy(io.Discard, amtSide)
	}}
	client := NewClient(dial.dial, "admin", "x", discard())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := client.Do(ctx, "TestAction", []byte("req"))
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(started), pipeTestTimeout)
	select {
	case <-dial.streams[0].closed:
	default:
		t.Fatal("Do left its stream open")
	}
}

func TestClientDo_RequestsOnOneClientRunSideBySide(t *testing.T) {
	release := make(chan struct{})
	var mu sync.Mutex
	waiting := 0
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		mu.Lock()
		waiting++
		if waiting == 2 {
			close(release)
		}
		mu.Unlock()
		// Each answer waits for the other request to arrive, so serial requests never finish.
		select {
		case <-release:
		case <-time.After(pipeTestTimeout):
		}
		return httpOK(minimalSOAPEnvelope(`<r:OK/>`))
	})

	errs := make(chan error, 2)
	for range 2 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, err := client.Do(ctx, "TestAction", []byte("req"))
			errs <- err
		}()
	}
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
}

func TestClientDo_DigestRetrySucceeds(t *testing.T) {
	body := minimalSOAPEnvelope(`<r:Response>authok</r:Response>`)
	var seenAuth string
	var mu sync.Mutex
	calls := 0
	client, _ := newWireFixture(t, func(req *http.Request) []byte {
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
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		calls++
		if calls == 1 {
			return httpUnauthorized("r", "n")
		}
		return []byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n")
	})

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wsman: HTTP 500")
}

func TestClientDo_MalformedHTTPResponseFails(t *testing.T) {
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return []byte("not an HTTP response at all\r\n")
	})

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read response")
}

func TestClientDo_OpenChannelFailurePropagates(t *testing.T) {
	dial := &fakeDial{openErr: fmt.Errorf("simulated open failure")}
	client := NewClient(dial.dial, "admin", "x", discard())

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "open channel")
	assert.Contains(t, err.Error(), "simulated open failure")
}

func TestClientDo_DigestChallengeRejected(t *testing.T) {
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return []byte("HTTP/1.1 401 Unauthorized\r\n" +
			"WWW-Authenticate: Basic realm=\"x\"\r\n" +
			"Content-Length: 0\r\n\r\n")
	})

	_, err := client.Do(context.Background(), "TestAction", []byte("req"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest auth")
}

func TestRequestPowerStateChange_SendsExpectedAction(t *testing.T) {
	body := minimalSOAPEnvelope(`<r:OK/>`)
	var seenAction string
	var mu sync.Mutex
	client, _ := newWireFixture(t, func(req *http.Request) []byte {
		mu.Lock()
		seenAction = req.URL.Path
		mu.Unlock()
		return httpOK(body)
	})

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

	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})

	info, err := client.GetDeviceInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "amt-host-7", info.Hostname)
	assert.Equal(t, "Optiplex 9020", info.Model)
}

func TestGetDeviceInfo_MalformedEnvelopeReturnsEmptyInfo(t *testing.T) {
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK("not xml at all <<<<")
	})

	info, err := client.GetDeviceInfo(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "", info.Hostname)
	assert.Equal(t, "", info.Model)
}

func TestGetPowerState_ParsesEnabledState(t *testing.T) {
	respBody := minimalSOAPEnvelope(`<p:EnabledState>2</p:EnabledState>`)
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})

	state, err := client.GetPowerState(context.Background())
	require.NoError(t, err)
	assert.Equal(t, PowerOn, state)
}

func TestGetPowerState_NonNumericEnabledStateReturnsError(t *testing.T) {
	respBody := minimalSOAPEnvelope(`<p:EnabledState>oops</p:EnabledState>`)
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(respBody)
	})

	_, err := client.GetPowerState(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse EnabledState")
}

func TestGetPowerState_EmptyBodyReturnsError(t *testing.T) {
	client, _ := newWireFixture(t, func(_ *http.Request) []byte {
		return httpOK(`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"></s:Envelope>`)
	})

	_, err := client.GetPowerState(context.Background())
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "empty soap body")
}
