package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// channelOpenTimeout bounds how long an open waits for the device's answer.
const channelOpenTimeout = 10 * time.Second

// errOpenTimedOut is returned when the device never answers a channel open.
var errOpenTimedOut = errors.New("apf: channel open not answered")

// BoundPort represents a port registered by the AMT device via tcpip-forward.
type BoundPort struct {
	Address string
	Port    uint32
}

// Conn represents a CIRA connection from an Intel AMT device. Only its message loop reads the
// network connection; channel opens wait for the loop to route the device's answer.
type Conn struct {
	AMTUUID    uuid.UUID
	BoundPorts []BoundPort
	netConn    net.Conn
	channels   map[uint32]*Channel
	opening    map[uint32]chan openReply
	closed     bool
	nextChanID uint32
	mu         sync.RWMutex
	logger     *slog.Logger

	// deviceID and tenantID identify the managed device this connection resolved to; both
	// are zero until a device reports the same SMBIOS system UUID.
	deviceID uuid.UUID
	tenantID uuid.UUID
}

// link records the device and tenant this connection resolved to.
func (c *Conn) link(deviceID, tenantID uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deviceID = deviceID
	c.tenantID = tenantID
}

// linked returns the resolved device and tenant, and whether the connection is linked.
func (c *Conn) linked() (uuid.UUID, uuid.UUID, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.deviceID, c.tenantID, c.deviceID != uuid.Nil
}

// NetConn returns the underlying network connection.
func (c *Conn) NetConn() net.Conn {
	return c.netConn
}

// openReply is the device's answer to a channel open, as the message loop routes it.
type openReply struct {
	channel *Channel
	err     error
}

// Close terminates the CIRA connection, ending every channel and every open still waiting.
func (c *Conn) Close() error {
	c.mu.Lock()
	channels := c.channels
	opening := c.opening
	c.channels = nil
	c.opening = nil
	c.closed = true
	c.mu.Unlock()
	for _, ch := range channels {
		ch.end(net.ErrClosed)
	}
	for _, reply := range opening {
		reply <- openReply{err: net.ErrClosed}
	}
	return c.netConn.Close()
}

// OpenChannel opens a direct-tcpip channel to targetAddr:targetPort on the device and waits up
// to channelOpenTimeout, or until ctx ends, for the device to answer.
func (c *Conn) OpenChannel(ctx context.Context, targetAddr string, targetPort uint16) (*Channel, error) {
	reply := make(chan openReply, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, net.ErrClosed
	}
	localCh := c.nextChanID
	c.nextChanID++
	if c.opening == nil {
		c.opening = make(map[uint32]chan openReply)
	}
	c.opening[localCh] = reply
	c.mu.Unlock()

	if err := writeChannelOpenDirect(c.netConn, localCh, targetAddr, targetPort); err != nil {
		c.abandonOpen(localCh, reply)
		return nil, fmt.Errorf("write channel open: %w", err)
	}

	timer := time.NewTimer(channelOpenTimeout)
	defer timer.Stop()
	select {
	case r := <-reply:
		return r.channel, r.err
	case <-ctx.Done():
		c.abandonOpen(localCh, reply)
		return nil, ctx.Err()
	case <-timer.C:
		c.abandonOpen(localCh, reply)
		return nil, errOpenTimedOut
	}
}

// abandonOpen stops waiting for localCh; a channel the loop opened in the meantime is closed.
func (c *Conn) abandonOpen(localCh uint32, reply chan openReply) {
	c.mu.Lock()
	delete(c.opening, localCh)
	c.mu.Unlock()
	select {
	case r := <-reply:
		if r.channel != nil {
			if err := r.channel.Close(); err != nil {
				c.logger.Debug("close abandoned channel", "channel", localCh, "error", err)
			}
		}
	default:
	}
}

// confirmOpen routes a ChannelOpenConfirm to the open waiting for it; a confirm nobody waits for
// is closed at once, so the device keeps no channel the server does not.
func (c *Conn) confirmOpen(payload []byte) error {
	if len(payload) < 12 {
		return ErrMessageTooShort
	}
	localCh := binary.BigEndian.Uint32(payload[0:4])
	remoteCh := binary.BigEndian.Uint32(payload[4:8])
	window := binary.BigEndian.Uint32(payload[8:12])

	c.mu.Lock()
	reply, waiting := c.opening[localCh]
	delete(c.opening, localCh)
	var ch *Channel
	if waiting {
		ch = newChannel(c, localCh, remoteCh, "direct-tcpip", window)
		c.channels[localCh] = ch
	}
	c.mu.Unlock()

	if !waiting {
		return WriteChannelClose(c.netConn, remoteCh)
	}
	reply <- openReply{channel: ch}
	return nil
}

// refuseOpen routes a ChannelOpenFailure to the open waiting for it.
func (c *Conn) refuseOpen(payload []byte) error {
	if len(payload) < 8 {
		return ErrMessageTooShort
	}
	localCh := binary.BigEndian.Uint32(payload[0:4])
	reason := binary.BigEndian.Uint32(payload[4:8])

	c.mu.Lock()
	reply, waiting := c.opening[localCh]
	delete(c.opening, localCh)
	c.mu.Unlock()

	if waiting {
		reply <- openReply{err: fmt.Errorf("channel open rejected: reason %d", reason)}
	}
	return nil
}

// channel returns the open channel with this local id.
func (c *Conn) channel(localCh uint32) (*Channel, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ch, ok := c.channels[localCh]
	return ch, ok
}

// forget removes a channel the server closed.
func (c *Conn) forget(localCh uint32) {
	c.mu.Lock()
	delete(c.channels, localCh)
	c.mu.Unlock()
}

// writeChannelOpenDirect writes an APF channel open for "direct-tcpip".
func writeChannelOpenDirect(w io.Writer, senderCh uint32, addr string, port uint16) error {
	chType := "direct-tcpip"
	// Layout: type, sender channel, window, max packet, connected addr and port, origin addr and port.
	addrBytes := encodeAPFString(addr)
	originBytes := encodeAPFString("0.0.0.0")

	totalLen := 1 + 4 + len(chType) + 12 + len(addrBytes) + 4 + len(originBytes) + 4
	buf := make([]byte, totalLen)
	off := 0
	buf[off] = APFChannelOpen
	off++
	// #nosec G115 -- chType is a fixed literal "direct-tcpip" (12 bytes).
	binary.BigEndian.PutUint32(buf[off:], uint32(len(chType)))
	off += 4
	copy(buf[off:], chType)
	off += len(chType)
	binary.BigEndian.PutUint32(buf[off:], senderCh)
	off += 4
	binary.BigEndian.PutUint32(buf[off:], DefaultWindowSize)
	off += 4
	binary.BigEndian.PutUint32(buf[off:], DefaultMaxPacketSize)
	off += 4
	copy(buf[off:], addrBytes)
	off += len(addrBytes)
	binary.BigEndian.PutUint32(buf[off:], uint32(port))
	off += 4
	copy(buf[off:], originBytes)
	off += len(originBytes)
	binary.BigEndian.PutUint32(buf[off:], 0) // origin port

	_, err := w.Write(buf)
	return err
}

func encodeAPFString(s string) []byte {
	if len(s) > maxAPFStringLen {
		s = s[:maxAPFStringLen]
	}
	buf := make([]byte, 4+len(s))
	binary.BigEndian.PutUint32(buf, uint32(len(s))) // #nosec G115 -- bounded above by maxAPFStringLen.
	copy(buf[4:], s)
	return buf
}
