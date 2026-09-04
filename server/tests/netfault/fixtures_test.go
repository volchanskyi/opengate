// The fixtures every test in this package shares: a fixed instant to measure
// from, one datagram's worth of bytes, and a stand-in server that echoes.
//
// They live together because the impairment tests and the forwarding tests
// measure the same shaper from two sides — one through pure decisions, one
// through real sockets — and duplicating the scaffolding would let the two
// drift into testing subtly different things.
package main

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// base is a fixed instant every test below measures from. The impairments take
// the time as an argument rather than reading a clock, so a test states the
// passage of time instead of sleeping through it.
var base = time.Date(2026, 9, 4, 6, 0, 0, 0, time.UTC)

// datagramBits is one QUIC datagram's worth of bits at the 1200-byte size the
// agent's transport settles on, so the rate tests read in the units the link
// is described in rather than in bytes.
const datagramBytes = 1200

// readDeadline bounds every read a test does against a real socket. It is a
// test's patience, not a property of the shaper: nothing here waits for a
// timer, so a read that has not answered inside it is a hang rather than a
// slow machine.
const readDeadline = 5 * time.Second

// echoServer is a stand-in for the real server: a UDP socket that sends back
// whatever it is given, from the address it was given it at. It is what makes
// the forwarding tests in-process — no cluster, no agent, no certificates.
type echoServer struct {
	conn *net.UDPConn
	// seen records every source address the echo answered, which is how a test
	// asserts the shaper opened one server-facing socket per machine.
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

// startShaper stands a shaper up in front of an echo server and returns it
// alongside the address a machine dials.
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

// machine dials the shaper the way an agent's transport would.
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
