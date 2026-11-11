package agentapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// headerOnly is a stream holding a control frame header that declares length and no payload; a
// reader that allocated before checking would ask it for length bytes.
type headerOnly struct {
	header *bytes.Reader
	asked  int
}

func newHeaderOnly(length uint32) *headerOnly {
	var h [5]byte
	h[0] = protocol.FrameControl
	binary.BigEndian.PutUint32(h[1:], length)
	return &headerOnly{header: bytes.NewReader(h[:])}
}

func (h *headerOnly) Read(p []byte) (int, error) {
	if h.header.Len() > 0 {
		return h.header.Read(p)
	}
	h.asked += len(p)
	return 0, io.EOF
}

func (h *headerOnly) Write(p []byte) (int, error) { return len(p), nil }

func TestAMessageDeclaredLargerThanAnyMachineSendsIsRefusedBeforeItIsRead(t *testing.T) {
	stream := newHeaderOnly(maxMachineMessageBytes + 1)
	ac := &AgentConn{DeviceID: uuid.New(), stream: stream, codec: &protocol.Codec{}, logger: testLogger()}

	err := ac.handleControl(context.Background())

	require.ErrorIs(t, err, protocol.ErrFrameTooLarge)
	assert.Zero(t, stream.asked, "nothing past the header is read for a refused length")
}

func TestAReaderPastTheAllowanceWaitsUntilItFrees(t *testing.T) {
	allowance := newReadAllowance(1024)
	release, err := allowance.reserve(context.Background(), 1024)
	require.NoError(t, err)

	reserved := make(chan func(), 1)
	go func() {
		next, err := allowance.reserve(context.Background(), 512)
		assert.NoError(t, err)
		reserved <- next
	}()
	select {
	case <-reserved:
		t.Fatal("a message was read past the allowance")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	select {
	case next := <-reserved:
		next()
	case <-time.After(2 * time.Second):
		t.Fatal("the waiting reader never proceeded once the allowance freed")
	}
}

func TestAWaitingReaderGivesUpWhenItsConnectionEnds(t *testing.T) {
	allowance := newReadAllowance(16)
	held, err := allowance.reserve(context.Background(), 16)
	require.NoError(t, err)
	defer held()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = allowance.reserve(ctx, 8)
	require.True(t, errors.Is(err, context.Canceled))
}

func TestAMessageHoldsItsShareOfTheAllowanceUntilItsHandlerReturns(t *testing.T) {
	allowance := newReadAllowance(maxMachineMessageBytes)
	ac, buf := newTestAgentConn(t, uuid.New(), nil)
	ac.allowance = allowance
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{Type: protocol.MsgAgentHeartbeat, Timestamp: 1})

	require.NoError(t, ac.handleControl(context.Background()))

	whole, err := allowance.reserve(context.Background(), maxMachineMessageBytes)
	require.NoError(t, err, "a handled message gives its share back")
	whole()
}
