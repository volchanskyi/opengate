package transport

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	client, peer := newDeadlinePipe(t)
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
		_, _, _ = ReadMessage(peer)
		confirm := make([]byte, 17)
		confirm[0] = APFChannelOpenConfirm
		binary.BigEndian.PutUint32(confirm[5:], 42)
		binary.BigEndian.PutUint32(confirm[9:], 0x4000)
		binary.BigEndian.PutUint32(confirm[13:], 0x8000)
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
		fail := make([]byte, 9)
		fail[0] = APFChannelOpenFailure
		binary.BigEndian.PutUint32(fail[5:], 7)
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
		other := make([]byte, 5) // keepalive reply carrying a cookie
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
	_ = peer.Close()
	_ = c.netConn.Close()

	ch, err := c.OpenChannel("10.0.0.1", 22)
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "write channel open")
}
