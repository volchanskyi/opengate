package transport

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wsmanPort = 16992

type openResult struct {
	ch  *Channel
	err error
}

// deviceConn is the server's connection to a device that has dialled in, and the device's end.
func deviceConn(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	srv, device, amtUUID := connectedAMT(t)
	return requireConn(t, srv, amtUUID), device
}

// openAsync starts an OpenChannel from the server side and returns where its result lands.
func openAsync(ctx context.Context, mc *Conn) <-chan openResult {
	done := make(chan openResult, 1)
	go func() {
		ch, err := mc.OpenChannel(ctx, "127.0.0.1", wsmanPort)
		done <- openResult{ch, err}
	}()
	return done
}

// expectOpen reads the device's next message as a channel open and returns the server's id.
func expectOpen(t *testing.T, device net.Conn) uint32 {
	t.Helper()
	raw := expectMessage(t, device, APFChannelOpen)
	co, err := ParseChannelOpen(raw)
	require.NoError(t, err)
	assert.Equal(t, "direct-tcpip", co.ChannelType)
	return co.SenderChannel
}

func sendRaw(t *testing.T, device net.Conn, msg []byte) {
	t.Helper()
	_, err := device.Write(msg)
	require.NoError(t, err)
}

func refuseOpen(t *testing.T, device net.Conn, local, reason uint32) {
	t.Helper()
	msg := []byte{APFChannelOpenFailure}
	msg = append(msg, encodeUint32(local)...)
	msg = append(msg, encodeUint32(reason)...)
	msg = append(msg, encodeAPFString("")...)
	msg = append(msg, encodeAPFString("")...)
	sendRaw(t, device, msg)
}

func sendKeepalive(t *testing.T, device net.Conn, cookie uint32) {
	t.Helper()
	sendRaw(t, device, append([]byte{APFKeepaliveRequest}, encodeUint32(cookie)...))
}

func expectKeepaliveReply(t *testing.T, device net.Conn, cookie uint32) {
	t.Helper()
	raw := expectMessage(t, device, APFKeepaliveReply)
	assert.Equal(t, cookie, binary.BigEndian.Uint32(raw))
}

func result(t *testing.T, done <-chan openResult) openResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(pipeTestTimeout):
		t.Fatal("OpenChannel never returned")
		return openResult{}
	}
}

// openedChannel opens a channel the device confirms with remote id `remote`.
func openedChannel(t *testing.T, mc *Conn, device net.Conn, remote uint32) *Channel {
	t.Helper()
	done := openAsync(context.Background(), mc)
	local := expectOpen(t, device)
	require.NoError(t, WriteChannelOpenConfirm(device, local, remote, DefaultWindowSize, DefaultMaxPacketSize))
	r := result(t, done)
	require.NoError(t, r.err)
	return r.ch
}

// readWithin reads once from ch, failing the test when the read blocks past `limit`.
func readWithin(t *testing.T, ch *Channel, buf []byte, limit time.Duration) (int, error) {
	t.Helper()
	type read struct {
		n   int
		err error
	}
	done := make(chan read, 1)
	go func() {
		n, err := ch.Read(buf)
		done <- read{n, err}
	}()
	select {
	case r := <-done:
		return r.n, r.err
	case <-time.After(limit):
		t.Fatal("the channel read blocked")
		return 0, nil
	}
}

func TestOpenChannelIsAnsweredThroughTheMessageLoop(t *testing.T) {
	mc, device := deviceConn(t)

	done := openAsync(context.Background(), mc)
	local := expectOpen(t, device)
	// A keepalive ahead of the answer is the message loop's to answer, and leaves the open waiting.
	sendKeepalive(t, device, 9)
	expectKeepaliveReply(t, device, 9)
	require.NoError(t, WriteChannelOpenConfirm(device, local, 42, 0x4000, DefaultMaxPacketSize))

	r := result(t, done)
	require.NoError(t, r.err)
	assert.Equal(t, uint32(42), r.ch.RemoteID)
	assert.Equal(t, local, r.ch.LocalID)
	assert.Equal(t, int64(0x4000), sendWindow(r.ch))
	assert.Equal(t, 1, channelCount(mc))
}

func TestOpenChannelRefusedByTheDevice(t *testing.T) {
	mc, device := deviceConn(t)

	done := openAsync(context.Background(), mc)
	refuseOpen(t, device, expectOpen(t, device), 7)

	r := result(t, done)
	require.Error(t, r.err)
	assert.Nil(t, r.ch)
	assert.Contains(t, r.err.Error(), "channel open rejected: reason 7")
	assert.Equal(t, 0, channelCount(mc))
}

func TestConcurrentOpensEachGetTheirOwnChannel(t *testing.T) {
	mc, device := deviceConn(t)

	first := openAsync(context.Background(), mc)
	firstLocal := expectOpen(t, device)
	second := openAsync(context.Background(), mc)
	secondLocal := expectOpen(t, device)
	require.NotEqual(t, firstLocal, secondLocal)

	require.NoError(t, WriteChannelOpenConfirm(device, secondLocal, 200, DefaultWindowSize, DefaultMaxPacketSize))
	require.NoError(t, WriteChannelOpenConfirm(device, firstLocal, 100, DefaultWindowSize, DefaultMaxPacketSize))
	a, b := result(t, first), result(t, second)
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	assert.Equal(t, uint32(100), a.ch.RemoteID)
	assert.Equal(t, uint32(200), b.ch.RemoteID)

	require.NoError(t, WriteChannelData(device, secondLocal, []byte("second")))
	require.NoError(t, WriteChannelData(device, firstLocal, []byte("first")))
	buf := make([]byte, 16)
	n, err := readWithin(t, a.ch, buf, pipeTestTimeout)
	require.NoError(t, err)
	assert.Equal(t, "first", string(buf[:n]))
	n, err = readWithin(t, b.ch, buf, pipeTestTimeout)
	require.NoError(t, err)
	assert.Equal(t, "second", string(buf[:n]))
}

func TestOpenChannelHonoursItsContext(t *testing.T) {
	mc, device := deviceConn(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := openAsync(ctx, mc)
	local := expectOpen(t, device)
	cancel()
	r := result(t, done)
	require.ErrorIs(t, r.err, context.Canceled)

	// A confirm arriving after the caller left is closed at once, so the device keeps no channel.
	require.NoError(t, WriteChannelOpenConfirm(device, local, 77, DefaultWindowSize, DefaultMaxPacketSize))
	raw := expectMessage(t, device, APFChannelClose)
	assert.Equal(t, uint32(77), binary.BigEndian.Uint32(raw))
	assert.Equal(t, 0, channelCount(mc))
}

func TestOpenChannelOnAClosedConnectionFails(t *testing.T) {
	srv, _, amtUUID := connectedAMT(t)
	mc := requireConn(t, srv, amtUUID)
	require.NoError(t, mc.Close())

	ch, err := mc.OpenChannel(context.Background(), "127.0.0.1", wsmanPort)
	require.Error(t, err)
	assert.Nil(t, ch)
}

func TestAReadReturnsTheDataThenEndsWhenTheDeviceCloses(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 5)

	require.NoError(t, WriteChannelData(device, ch.LocalID, []byte("reply")))
	require.NoError(t, WriteChannelClose(device, ch.LocalID))
	raw := expectMessage(t, device, APFChannelClose)
	assert.Equal(t, uint32(5), binary.BigEndian.Uint32(raw))

	got, err := io.ReadAll(ch)
	require.NoError(t, err)
	assert.Equal(t, "reply", string(got))
	assert.Equal(t, 0, channelCount(mc))
}

func TestClosingAChannelEndsItsReadAndTellsTheDevice(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 6)

	pending := make(chan error, 1)
	go func() {
		_, err := ch.Read(make([]byte, 8))
		pending <- err
	}()
	require.NoError(t, ch.Close())
	raw := expectMessage(t, device, APFChannelClose)
	assert.Equal(t, uint32(6), binary.BigEndian.Uint32(raw))

	select {
	case err := <-pending:
		require.ErrorIs(t, err, net.ErrClosed)
	case <-time.After(pipeTestTimeout):
		t.Fatal("the pending read never returned")
	}
	_, err := ch.Write([]byte("late"))
	require.ErrorIs(t, err, net.ErrClosed)
	assert.Equal(t, 0, channelCount(mc))
}

func TestALostConnectionEndsEveryPendingRead(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 8)

	require.NoError(t, device.Close())
	_, err := readWithin(t, ch, make([]byte, 8), pipeTestTimeout)
	require.Error(t, err)
}

func TestAnUnreadChannelNeverStallsTheMessageLoop(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 9)

	// The device fills the whole window it was given while nobody reads.
	require.NoError(t, WriteChannelData(device, ch.LocalID, make([]byte, DefaultWindowSize)))
	sendKeepalive(t, device, 3)
	expectKeepaliveReply(t, device, 3)
}

func TestReadingIsWhatReopensTheWindow(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 10)

	data := bytes.Repeat([]byte("w"), int(DefaultWindowSize/2)+1)
	require.NoError(t, WriteChannelData(device, ch.LocalID, data))
	// Messages answer in order, so a keepalive reply ahead of any adjust shows none went out yet.
	sendKeepalive(t, device, 4)
	expectKeepaliveReply(t, device, 4)

	got := make([]byte, len(data))
	_, err := io.ReadFull(ch, got)
	require.NoError(t, err)
	raw := expectMessage(t, device, APFChannelWindowAdj)
	assert.Equal(t, uint32(10), binary.BigEndian.Uint32(raw[0:4]))
	assert.Equal(t, uint32(len(data)), binary.BigEndian.Uint32(raw[4:8]))
}

func TestADeviceOverrunningItsWindowLosesTheChannel(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 11)

	require.NoError(t, WriteChannelData(device, ch.LocalID, make([]byte, DefaultWindowSize)))
	require.NoError(t, WriteChannelData(device, ch.LocalID, []byte("x")))
	raw := expectMessage(t, device, APFChannelClose)
	assert.Equal(t, uint32(11), binary.BigEndian.Uint32(raw))

	got, err := io.ReadAll(ch)
	require.ErrorIs(t, err, errWindowExceeded)
	assert.Len(t, got, int(DefaultWindowSize))
	// The connection outlives the channel.
	sendKeepalive(t, device, 5)
	expectKeepaliveReply(t, device, 5)
}

func TestAWriteIsSplitIntoPacketsTheDeviceAccepts(t *testing.T) {
	mc, device := deviceConn(t)
	ch := openedChannel(t, mc, device, 12)

	payload := bytes.Repeat([]byte("p"), int(DefaultMaxPacketSize)+10)
	written := make(chan error, 1)
	go func() {
		_, err := ch.Write(payload)
		written <- err
	}()

	var got []byte
	for len(got) < len(payload) {
		raw := expectMessage(t, device, APFChannelData)
		cd, err := ParseChannelData(raw)
		require.NoError(t, err)
		assert.Equal(t, uint32(12), cd.RecipientChannel)
		assert.LessOrEqual(t, len(cd.Data), int(DefaultMaxPacketSize))
		got = append(got, cd.Data...)
	}
	require.NoError(t, <-written)
	assert.Equal(t, payload, got)
}
