package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// scriptedConn is a net.Conn whose reads replay the frame armed before each read and whose
// writes are discarded, so a relayed message is measured with no peer goroutine or socket.
type scriptedConn struct {
	frame bytes.Reader
}

func (c *scriptedConn) Read(p []byte) (int, error)       { return c.frame.Read(p) }
func (c *scriptedConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *scriptedConn) Close() error                     { return nil }
func (c *scriptedConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (c *scriptedConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (c *scriptedConn) SetDeadline(time.Time) error      { return nil }
func (c *scriptedConn) SetReadDeadline(time.Time) error  { return nil }
func (c *scriptedConn) SetWriteDeadline(time.Time) error { return nil }

// hijackableRecorder hands Accept the scripted connection, as net/http hands it a socket.
type hijackableRecorder struct {
	*httptest.ResponseRecorder
	conn net.Conn
}

func (r *hijackableRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return r.conn, bufio.NewReadWriter(bufio.NewReader(r.conn), bufio.NewWriter(r.conn)), nil
}

// scriptedWSConn returns a relay connection over conn, accepted with the options the relay
// handler's upgrade uses.
func scriptedWSConn(b *testing.B, conn net.Conn) *WSConn {
	b.Helper()
	req := httptest.NewRequest(http.MethodGet, testPathWSRelay+"bench"+testSideAgent, nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	w := &hijackableRecorder{ResponseRecorder: httptest.NewRecorder(), conn: conn}
	ws, err := websocket.Accept(w, req, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		b.Fatal(err)
	}
	return NewWSConn(ws, "bench")
}

// maskedBinaryFrame encodes payload as one masked binary frame, the form a client sends; the
// lengths the benchmarks use take the 7-bit and the 64-bit length forms.
func maskedBinaryFrame(payload []byte) []byte {
	frame := []byte{0x82}
	if len(payload) < 126 {
		frame = append(frame, 0x80|byte(len(payload)))
	} else {
		frame = append(frame, 0x80|127)
		frame = binary.BigEndian.AppendUint64(frame, uint64(len(payload)))
	}
	key := [4]byte{0x1f, 0x2e, 0x3d, 0x4c}
	frame = append(frame, key[:]...)
	for i, v := range payload {
		frame = append(frame, v^key[i%len(key)])
	}
	return frame
}

// benchRelayForward times one message of size bytes through the relay's copy step: read whole
// from the agent's side, written whole to the browser's.
func benchRelayForward(b *testing.B, size int) {
	frame := maskedBinaryFrame(make([]byte, size))
	agentSide := &scriptedConn{}
	agent := scriptedWSConn(b, agentSide)
	browser := scriptedWSConn(b, &scriptedConn{})

	b.SetBytes(int64(size))
	b.ReportAllocs()
	for b.Loop() {
		agentSide.frame.Reset(frame)
		msg, err := agent.ReadMessage()
		if err != nil {
			b.Fatal(err)
		}
		err = browser.WriteMessage(msg.Bytes())
		msg.Release()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRelayForward_Keystroke(b *testing.B) { benchRelayForward(b, 64) }

func BenchmarkRelayForward_FilePiece(b *testing.B) { benchRelayForward(b, 256<<10) }
