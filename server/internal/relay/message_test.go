package relay

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func patterned(size int, seed byte) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = seed + byte(i%251)
	}
	return data
}

func TestReadMessageFromKeepsEveryByteAcrossTheSizeClasses(t *testing.T) {
	for _, size := range []int{
		0, 1, 64 << 10, 64<<10 + 1, 256 << 10, 256<<10 + 1, 1 << 20, 1<<20 + 1, MaxMessageSize,
	} {
		want := patterned(size, byte(size))
		msg, err := ReadMessageFrom(bytes.NewReader(want))
		require.NoError(t, err, "size %d", size)
		assert.Equal(t, want, msg.Bytes(), "size %d", size)
		msg.Release()
	}
}

func TestReadMessageFromKeepsEveryByteOfAMessageArrivingInPieces(t *testing.T) {
	want := patterned(64<<10+3, 9)
	msg, err := ReadMessageFrom(iotest.OneByteReader(bytes.NewReader(want)))
	require.NoError(t, err)
	defer msg.Release()
	assert.Equal(t, want, msg.Bytes())
}

func TestReadMessageFromRefusesAMessagePastTheLimit(t *testing.T) {
	_, err := ReadMessageFrom(bytes.NewReader(make([]byte, MaxMessageSize+1)))
	require.ErrorIs(t, err, ErrMessageTooLarge)
}

func TestReadMessageFromPassesOnAReadError(t *testing.T) {
	_, err := ReadMessageFrom(iotest.ErrReader(io.ErrClosedPipe))
	require.ErrorIs(t, err, io.ErrClosedPipe)
}

func TestAReleasedBufferServesTheNextMessageWithNoTrace(t *testing.T) {
	large := patterned(60<<10, 7)
	msg, err := ReadMessageFrom(bytes.NewReader(large))
	require.NoError(t, err)
	msg.Release()

	small, err := ReadMessageFrom(bytes.NewReader([]byte("ok")))
	require.NoError(t, err)
	defer small.Release()
	assert.Equal(t, []byte("ok"), small.Bytes())
}

func TestRelayForwardsLargeSmallLargeByteForByte(t *testing.T) {
	r := NewRelay(slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, agentLocal, browserLocal := registerSession(t, r)

	sequence := [][]byte{
		patterned(300<<10, 1), []byte("small"), patterned(200<<10, 2), patterned(70<<10, 3), []byte("x"),
	}
	for _, msg := range sequence {
		require.NoError(t, agentLocal.WriteMessage(msg))
	}
	for i, want := range sequence {
		got := readWithin(t, browserLocal, 5*time.Second)
		assert.Equalf(t, want, got, "message %d", i)
	}
}

// readWithin reads one message from conn and copies it out of its buffer.
func readWithin(t *testing.T, conn Conn, limit time.Duration) []byte {
	t.Helper()
	done := make(chan []byte, 1)
	go func() {
		msg, err := conn.ReadMessage()
		if err != nil {
			done <- nil
			return
		}
		done <- append([]byte(nil), msg.Bytes()...)
		msg.Release()
	}()
	select {
	case data := <-done:
		require.NotNil(t, data, "the read failed")
		return data
	case <-time.After(limit):
		t.Fatal("no message arrived")
		return nil
	}
}
