package api

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// endingServer accepts one WebSocket, reads it as the relay's copier does, and ends it with reason.
func endingServer(t *testing.T, reason string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn := NewWSConn(raw, "browser")
		reading := make(chan struct{})
		go func() {
			defer close(reading)
			for {
				if _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()
		time.Sleep(50 * time.Millisecond)
		_ = conn.End(reason)
		<-reading
		_ = raw.CloseNow()
	}))
}

type closeFrame struct {
	code   uint16
	reason string
}

// rawCloseFrames upgrades a plain TCP connection, echoes the first close frame as a browser does,
// and returns every close frame the server wrote until it hung up.
func rawCloseFrames(t *testing.T, serverURL string) []closeFrame {
	t.Helper()
	tcp, err := net.Dial("tcp", strings.TrimPrefix(serverURL, "http://"))
	require.NoError(t, err)
	defer tcp.Close()
	require.NoError(t, tcp.SetDeadline(time.Now().Add(5*time.Second)))

	_, err = fmt.Fprintf(tcp, "GET / HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n",
		strings.TrimPrefix(serverURL, "http://"))
	require.NoError(t, err)
	r := bufio.NewReader(tcp)
	resp, err := http.ReadResponse(r, nil)
	require.NoError(t, err)
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	var closes []closeFrame
	for {
		var head [2]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return closes
		}
		length := int(head[1] & 0x7f)
		switch length {
		case 126:
			var ext [2]byte
			_, err = io.ReadFull(r, ext[:])
			length = int(binary.BigEndian.Uint16(ext[:]))
		case 127:
			var ext [8]byte
			_, err = io.ReadFull(r, ext[:])
			length = int(binary.BigEndian.Uint64(ext[:]))
		}
		require.NoError(t, err)
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return closes
		}
		if head[0]&0x0f != 0x8 {
			continue
		}
		frame := closeFrame{}
		if len(payload) >= 2 {
			frame = closeFrame{code: binary.BigEndian.Uint16(payload), reason: string(payload[2:])}
		}
		closes = append(closes, frame)
		if len(closes) == 1 {
			// A client's frame is masked; a zero key leaves the payload as it is.
			echo := append([]byte{0x88, 0x80 | byte(len(payload)), 0, 0, 0, 0}, payload...)
			_, err = tcp.Write(echo)
			require.NoError(t, err)
		}
	}
}

func TestWSConn_EndWritesOneCloseFrame(t *testing.T) {
	t.Parallel()
	srv := endingServer(t, "Ended by Carol")
	defer srv.Close()

	closes := rawCloseFrames(t, srv.URL)

	require.Len(t, closes, 1, "a browser fails the connection on a second close frame and drops the reason")
	assert.Equal(t, uint16(StatusSessionEnded), closes[0].code)
	assert.Equal(t, "Ended by Carol", closes[0].reason)
}
