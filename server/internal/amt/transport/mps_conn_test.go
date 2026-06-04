package transport

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the client-side Conn channel-open path (mps_conn.go). The shared
// server/handshake harness lives in mps_test.go (same package).

// newTestConn builds a Conn over one end of a net.Pipe for white-box tests of
// the client-side channel-open path. The returned peer is the AMT-device end.
func newTestConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	c := &Conn{
		netConn:  client,
		channels: make(map[uint32]*Channel),
		logger:   discardLogger(),
	}
	return c, peer
}

func TestOpenChannelConfirm(t *testing.T) {
	c, peer := newTestConn(t)

	go func() {
		// Consume the direct-tcpip channel-open request, then confirm it.
		_, _, _ = ReadMessage(peer)
		confirm := make([]byte, 17) // type + recipient + sender + window + maxpkt
		confirm[0] = APFChannelOpenConfirm
		binary.BigEndian.PutUint32(confirm[5:], 42)      // sender (our remote channel id)
		binary.BigEndian.PutUint32(confirm[9:], 0x4000)  // initial window
		binary.BigEndian.PutUint32(confirm[13:], 0x8000) // max packet
		_, _ = peer.Write(confirm)
	}()

	ch, err := c.OpenChannel("10.0.0.1", 22)
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, uint32(42), ch.RemoteID)
	assert.Equal(t, "direct-tcpip", ch.Type)
	assert.Equal(t, int64(0x4000), ch.sendWindow)

	c.mu.Lock()
	stored, ok := c.channels[ch.LocalID]
	c.mu.Unlock()
	assert.True(t, ok)
	assert.Same(t, ch, stored)
}

func TestOpenChannelRejected(t *testing.T) {
	c, peer := newTestConn(t)

	go func() {
		_, _, _ = ReadMessage(peer)
		fail := make([]byte, 9) // type + recipient + reason
		fail[0] = APFChannelOpenFailure
		binary.BigEndian.PutUint32(fail[5:], 7) // reason code
		_, _ = peer.Write(fail)
	}()

	ch, err := c.OpenChannel("10.0.0.1", 22)
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "channel open rejected")
	assert.Contains(t, err.Error(), "7")
}

func TestOpenChannelUnexpectedResponse(t *testing.T) {
	c, peer := newTestConn(t)

	go func() {
		_, _, _ = ReadMessage(peer)
		other := make([]byte, 5) // a keepalive reply (cookie) — not a channel response
		other[0] = APFKeepaliveReply
		_, _ = peer.Write(other)
	}()

	ch, err := c.OpenChannel("10.0.0.1", 22)
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "unexpected response type")
}

func TestOpenChannelWriteError(t *testing.T) {
	c, peer := newTestConn(t)
	// Close both ends so the channel-open write fails immediately.
	_ = peer.Close()
	_ = c.netConn.Close()

	ch, err := c.OpenChannel("10.0.0.1", 22)
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "write channel open")
}
