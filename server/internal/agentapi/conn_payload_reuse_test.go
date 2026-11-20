package agentapi

import (
	"bytes"
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// repeatingStream hands out the same frame again and again and takes every write.
type repeatingStream struct {
	frame []byte
	at    int
}

func (s *repeatingStream) Read(p []byte) (int, error) {
	n := copy(p, s.frame[s.at:])
	s.at = (s.at + n) % len(s.frame)
	return n, nil
}

func (s *repeatingStream) Write(p []byte) (int, error) { return len(p), nil }

// heapBytesAllocatedBy is what fn allocated on the heap.
func heapBytesAllocatedBy(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestAFramesPayloadIsReusedOnceItIsHandled(t *testing.T) {
	codec := &protocol.Codec{}
	payload, err := codec.EncodeControl(&protocol.ControlMessage{
		Type: protocol.MsgAgentAlert, Evidence: bytes.Repeat([]byte{0x5a}, 60<<10),
	})
	require.NoError(t, err)
	var frame bytes.Buffer
	require.NoError(t, codec.WriteFrame(&frame, protocol.FrameControl, payload))
	ac := &AgentConn{stream: &repeatingStream{frame: frame.Bytes()}, codec: codec}
	read := func() {
		frameType, got, release, err := ac.readFrame(context.Background())
		require.NoError(t, err)
		require.Equal(t, protocol.FrameControl, frameType)
		require.Len(t, got, len(payload))
		release()
	}
	read()

	const frames = 20
	allocated := heapBytesAllocatedBy(func() {
		for range frames {
			read()
		}
	}) / frames

	assert.Less(t, allocated, uint64(len(payload)/8), "a %d-byte payload is read into the last one's buffer",
		len(payload))
}
