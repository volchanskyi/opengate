package transport

import (
	"bytes"
	"errors"
	"io"
	"math"
	"net"
	"sync"
)

// errWindowExceeded ends a channel whose device sent more than the window it was given.
var errWindowExceeded = errors.New("apf: device sent past the channel window")

// Channel is one APF channel within a CIRA connection, read and written as a byte stream.
// Delivered data waits in a buffer the size of the advertised window until a read reopens it.
type Channel struct {
	LocalID  uint32
	RemoteID uint32
	Type     string

	conn *Conn
	// discard marks a channel the device opened, whose data has no reader and is acknowledged
	// as it arrives.
	discard bool

	mu         sync.Mutex
	sendWindow int64        // credits remaining for sending to remote
	inbox      bytes.Buffer // data delivered and not yet read, at most DefaultWindowSize
	unacked    int64        // bytes taken since the last window adjust
	err        error        // why the channel ended; reads return it once the inbox is empty
	ready      chan struct{}
}

func newChannel(c *Conn, local, remote uint32, chType string, window uint32) *Channel {
	return &Channel{
		LocalID:    local,
		RemoteID:   remote,
		Type:       chType,
		conn:       c,
		sendWindow: int64(window),
		ready:      make(chan struct{}, 1),
	}
}

// Read returns delivered data, blocking until some arrives or the channel ends.
func (ch *Channel) Read(p []byte) (int, error) {
	for {
		ch.mu.Lock()
		if ch.inbox.Len() > 0 {
			n, _ := ch.inbox.Read(p)
			// An ended channel takes what is left and reopens nothing.
			var adjust uint32
			if ch.err == nil {
				adjust = ch.takeAdjust(int64(n))
			}
			ch.mu.Unlock()
			ch.reopenWindow(adjust)
			return n, nil
		}
		if ch.err != nil {
			err := ch.err
			ch.mu.Unlock()
			return 0, err
		}
		ch.mu.Unlock()
		<-ch.ready
	}
}

// Write sends p to the device in packets no larger than DefaultMaxPacketSize.
func (ch *Channel) Write(p []byte) (int, error) {
	written := 0
	for written < len(p) {
		if err := ch.ended(); err != nil {
			return written, err
		}
		end := min(written+int(DefaultMaxPacketSize), len(p))
		if err := WriteChannelData(ch.conn.netConn, ch.RemoteID, p[written:end]); err != nil {
			return written, err
		}
		written = end
	}
	return written, nil
}

// Close ends the channel: a pending read returns, and the device is told.
func (ch *Channel) Close() error {
	if !ch.end(net.ErrClosed) {
		return nil
	}
	ch.conn.forget(ch.LocalID)
	return WriteChannelClose(ch.conn.netConn, ch.RemoteID)
}

// deliver takes data the message loop read for this channel and returns the window adjust to
// send for it, which only a channel nobody reads sends on arrival.
func (ch *Channel) deliver(data []byte) (uint32, error) {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.discard {
		return ch.takeAdjust(int64(len(data))), nil
	}
	if ch.err != nil {
		return 0, nil
	}
	if ch.inbox.Len()+len(data) > int(DefaultWindowSize) {
		return 0, errWindowExceeded
	}
	ch.inbox.Write(data)
	ch.signal()
	return 0, nil
}

// takeAdjust counts n bytes taken and returns the adjust due once they reach half the window.
// The caller holds ch.mu.
func (ch *Channel) takeAdjust(n int64) uint32 {
	ch.unacked += n
	if ch.unacked < int64(DefaultWindowSize)/2 {
		return 0
	}
	adjust := min(ch.unacked, math.MaxUint32)
	ch.unacked = 0
	return uint32(adjust)
}

func (ch *Channel) reopenWindow(adjust uint32) {
	if adjust == 0 {
		return
	}
	if err := WriteChannelWindowAdj(ch.conn.netConn, ch.RemoteID, adjust); err != nil {
		ch.conn.logger.Debug("write window adjust", "channel", ch.LocalID, "error", err)
	}
}

// end records why the channel ended and wakes a pending read; it reports whether this call
// ended it.
func (ch *Channel) end(err error) bool {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.err != nil {
		return false
	}
	ch.err = err
	ch.signal()
	return true
}

func (ch *Channel) ended() error {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	if ch.err != nil {
		return net.ErrClosed
	}
	return nil
}

// signal wakes the reader; the one-slot channel keeps one wake-up pending at most.
// The caller holds ch.mu.
func (ch *Channel) signal() {
	select {
	case ch.ready <- struct{}{}:
	default:
	}
}

var _ io.ReadWriteCloser = (*Channel)(nil)
