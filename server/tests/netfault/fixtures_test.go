package main

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// base is the fixed instant tests measure from; impairments take the time as an argument.
var base = time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)

const datagramBytes = 1200

const readDeadline = 5 * time.Second

// echoServer is a UDP stand-in for the server that sends back whatever it receives.
type echoServer struct {
	conn *net.UDPConn
	mu   sync.Mutex
	seen map[string]int
}

func newEchoServer(t *testing.T) *echoServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	e := &echoServer{conn: conn, seen: map[string]int{}}
	go e.serve()
	t.Cleanup(func() { _ = conn.Close() })
	return e
}

func (e *echoServer) serve() {
	buf := make([]byte, readBufferBytes)
	for {
		n, from, err := e.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		e.mu.Lock()
		e.seen[from.String()]++
		e.mu.Unlock()
		_, _ = e.conn.WriteToUDP(append([]byte("echo:"), buf[:n]...), from)
	}
}

func (e *echoServer) sources() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.seen)
}

func (e *echoServer) addr() *net.UDPAddr { return e.conn.LocalAddr().(*net.UDPAddr) }

func startShaper(t *testing.T, seed uint64) (*Shaper, *net.UDPAddr, *echoServer) {
	t.Helper()
	server := newEchoServer(t)
	shaper, err := NewShaper(Config{
		Listen:     "127.0.0.1:0",
		ServerAddr: server.addr().String(),
		Seed:       seed,
		IdleExpiry: mappingIdleExpiry,
	})
	require.NoError(t, err)
	go shaper.Serve()
	t.Cleanup(shaper.Close)
	return shaper, shaper.ListenAddr(), server
}

func machine(t *testing.T, to *net.UDPAddr) *net.UDPConn {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, to)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func exchange(t *testing.T, conn *net.UDPConn, payload string) (string, error) {
	t.Helper()
	_, err := conn.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(readDeadline)))
	buf := make([]byte, readBufferBytes)
	n, err := conn.Read(buf)
	if err != nil {
		return "", err
	}
	return string(buf[:n]), nil
}

// awaitForwarded waits for the counters because the forwarded count is recorded after the write,
// so a test holding the reply can read them first.
func awaitForwarded(t *testing.T, shaper *Shaper, toServer, toMachine int64) Counters {
	t.Helper()
	require.Eventually(t, func() bool {
		got := shaper.Counters()
		return got.ToServer.Out == toServer && got.ToMachine.Out == toMachine
	}, readDeadline, time.Millisecond,
		"the shaper did not count what it forwarded: want %d to the server and %d to the machine",
		toServer, toMachine)
	return shaper.Counters()
}
