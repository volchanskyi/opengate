package relay

import (
	"errors"
	"io"
	"sync"
)

// MaxMessageSize is the largest message the relay forwards (4 MiB).
const MaxMessageSize = 4 << 20

// ErrMessageTooLarge is returned for a message longer than MaxMessageSize.
var ErrMessageTooLarge = errors.New("relay: message longer than the limit")

// messageClasses are the buffer sizes a message is read into, smallest first. Each buffer holds
// one byte past its class, so a message that fills it is known to need the next class.
var messageClasses = [...]int{64 << 10, 256 << 10, 1 << 20, MaxMessageSize}

// messagePools hold one pool of buffers per class; an idle session holds none of them.
var messagePools [len(messageClasses)]sync.Pool

func init() {
	for i, size := range messageClasses {
		messagePools[i].New = func() any {
			buf := make([]byte, size+1)
			return &buf
		}
	}
}

// Message is one whole message in a buffer borrowed from the relay's pools; its bytes are valid
// until Release.
type Message struct {
	buf   *[]byte
	n     int
	class int
}

// Bytes returns the message.
func (m Message) Bytes() []byte {
	if m.buf == nil {
		return nil
	}
	return (*m.buf)[:m.n]
}

// Release returns the message's buffer to its pool.
func (m Message) Release() {
	if m.buf != nil {
		messagePools[m.class].Put(m.buf)
	}
}

// ReadMessageFrom reads r to its end, moving the message into the next class each time it fills
// a buffer.
func ReadMessageFrom(r io.Reader) (Message, error) {
	class := 0
	buf, _ := messagePools[class].Get().(*[]byte)
	n := 0
	for {
		if n == len(*buf) {
			if class == len(messageClasses)-1 {
				messagePools[class].Put(buf)
				return Message{}, ErrMessageTooLarge
			}
			next, _ := messagePools[class+1].Get().(*[]byte)
			copy(*next, (*buf)[:n])
			messagePools[class].Put(buf)
			buf, class = next, class+1
		}
		read, err := r.Read((*buf)[n:])
		n += read
		// io.Reader returns io.EOF itself at the end; a wrapped EOF is a connection lost mid-message.
		if err == io.EOF {
			return Message{buf: buf, n: n, class: class}, nil
		}
		if err != nil {
			messagePools[class].Put(buf)
			return Message{}, err
		}
	}
}
