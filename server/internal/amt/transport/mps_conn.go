package transport

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// This file holds the CIRA connection and channel types and their methods. The
// server core lives in mps.go; the handshake in mps_handshake.go; post-handshake
// message dispatch in mps_handlers.go.

// BoundPort represents a port registered by the AMT device via tcpip-forward.
type BoundPort struct {
	Address string
	Port    uint32
}

// Conn represents a CIRA connection from an Intel AMT device.
type Conn struct {
	AMTUUID    uuid.UUID
	BoundPorts []BoundPort
	netConn    net.Conn
	channels   map[uint32]*Channel
	nextChanID uint32
	mu         sync.RWMutex
	logger     *slog.Logger
}

// NetConn returns the underlying network connection.
func (c *Conn) NetConn() net.Conn {
	return c.netConn
}

// Close terminates the CIRA connection and all open channels.
func (c *Conn) Close() error {
	c.mu.Lock()
	for _, ch := range c.channels {
		ch.mu.Lock()
		if ch.fwd != nil {
			if err := ch.fwd.Close(); err != nil {
				c.logger.Debug("close forwarded channel during conn shutdown", "error", err)
			}
		}
		ch.mu.Unlock()
	}
	c.channels = nil
	c.mu.Unlock()
	return c.netConn.Close()
}

// OpenChannel opens a new channel to the AMT device for port forwarding.
// It sends a channel open request and waits for confirmation.
func (c *Conn) OpenChannel(targetAddr string, targetPort uint16) (*Channel, error) {
	c.mu.Lock()
	localCh := c.nextChanID
	c.nextChanID++
	c.mu.Unlock()

	// Build channel open message for "direct-tcpip".
	if err := writeChannelOpenDirect(c.netConn, localCh, targetAddr, targetPort); err != nil {
		return nil, fmt.Errorf("write channel open: %w", err)
	}

	// Read the response.
	if err := c.netConn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return nil, err
	}
	defer func() { _ = c.netConn.SetReadDeadline(time.Time{}) }()

	msgType, payload, err := ReadMessage(c.netConn)
	if err != nil {
		return nil, fmt.Errorf("read channel open response: %w", err)
	}

	if msgType == APFChannelOpenFailure {
		reason := uint32(0)
		if len(payload) >= 8 {
			reason = binary.BigEndian.Uint32(payload[4:8])
		}
		return nil, fmt.Errorf("channel open rejected: reason %d", reason)
	}

	if msgType != APFChannelOpenConfirm {
		return nil, fmt.Errorf("unexpected response type %d", msgType)
	}

	if len(payload) < 12 {
		return nil, ErrMessageTooShort
	}
	remoteCh := binary.BigEndian.Uint32(payload[4:8])
	windowSz := binary.BigEndian.Uint32(payload[8:12])

	ch := &Channel{
		LocalID:    localCh,
		RemoteID:   remoteCh,
		Type:       "direct-tcpip",
		sendWindow: int64(windowSz),
	}

	c.mu.Lock()
	c.channels[localCh] = ch
	c.mu.Unlock()

	return ch, nil
}

// Channel represents an APF channel within a CIRA connection.
type Channel struct {
	LocalID      uint32
	RemoteID     uint32
	Type         string
	fwd          net.Conn     // optional TCP forwarding connection
	OnData       func([]byte) // optional callback for received data (used by WSMAN ChannelConn)
	sendWindow   int64        // credits remaining for sending to remote
	recvConsumed int64        // bytes consumed since last WindowAdj
	mu           sync.Mutex
}

// SetOnData sets a callback for received channel data (used by WSMAN ChannelConn).
func (ch *Channel) SetOnData(fn func([]byte)) {
	ch.mu.Lock()
	ch.OnData = fn
	ch.mu.Unlock()
}

// writeChannelOpenDirect writes an APF channel open for "direct-tcpip".
func writeChannelOpenDirect(w io.Writer, senderCh uint32, addr string, port uint16) error {
	chType := "direct-tcpip"
	// Build: type_str + sender_ch + window + max_pkt + connected_addr + connected_port + origin_addr + origin_port
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
