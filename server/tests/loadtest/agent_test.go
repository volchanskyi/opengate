package main

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/protocol"
)

// deadlineLog is a stream that remembers every read deadline it was given.
type deadlineLog struct {
	io.Reader
	io.Writer
	deadlines []time.Time
}

func (d *deadlineLog) SetReadDeadline(t time.Time) error {
	d.deadlines = append(d.deadlines, t)
	return nil
}

func TestASignInLastsUntilTheServerAnswers(t *testing.T) {
	codec := &protocol.Codec{}
	answers, server := io.Pipe()
	var sent bytes.Buffer
	stream := &deadlineLog{Reader: answers, Writer: &sent}

	const serverTook = 150 * time.Millisecond
	go func() {
		time.Sleep(serverTook)
		var frame bytes.Buffer
		writeControl(t, codec, &frame, &protocol.ControlMessage{Type: protocol.MsgTrustBundle, CABundle: "ca"})
		_, _ = server.Write(frame.Bytes())
	}()

	began := time.Now()
	require.NoError(t, signIn(context.Background(), codec, stream, "host-1", loadOptions{}))

	assert.GreaterOrEqual(t, time.Since(began), serverTook,
		"a machine is online when the server answers, so a wait in the server is part of its sign-in")
	assert.Equal(t, protocol.MsgAgentRegister, readControl(t, codec, &sent).Type)
}

func TestTheSignInWaitLeavesNoDeadlineOnTheStream(t *testing.T) {
	codec := &protocol.Codec{}
	var answers bytes.Buffer
	writeControl(t, codec, &answers, &protocol.ControlMessage{Type: protocol.MsgTrustBundle, CABundle: "ca"})
	stream := &deadlineLog{Reader: &answers, Writer: &bytes.Buffer{}}

	require.NoError(t, signIn(context.Background(), codec, stream, "host-1", loadOptions{}))

	require.Len(t, stream.deadlines, 2)
	assert.WithinDuration(t, time.Now().Add(signInAnswerWait), stream.deadlines[0], time.Second,
		"the wait for the answer is the fleet-return promise's five minutes")
	assert.True(t, stream.deadlines[1].IsZero(),
		"catch-up reads its answers with no deadline, so the sign-in's own is cleared")
}

func TestAFrameThatAnswersTheSignInIsStillServed(t *testing.T) {
	codec := &protocol.Codec{}
	var answers, sent bytes.Buffer
	writeControl(t, codec, &answers, &protocol.ControlMessage{Type: protocol.MsgRequestDeviceLogs, LogLimit: 3})
	stream := &deadlineLog{Reader: &answers, Writer: &sent}

	require.NoError(t, signIn(context.Background(), codec, stream, "host-1", loadOptions{}))

	assert.Equal(t, protocol.MsgAgentRegister, readControl(t, codec, &sent).Type)
	assert.Equal(t, protocol.MsgDeviceLogsResponse, readControl(t, codec, &sent).Type,
		"a pull that arrives first is answered as a held machine answers it")
}

func TestAServerThatNeverAnswersLeavesTheMachineOffline(t *testing.T) {
	stream := &deadlineBuffer{in: &bytes.Buffer{}, out: &bytes.Buffer{}}

	err := signIn(context.Background(), &protocol.Codec{}, stream, "host-1", loadOptions{})

	require.Error(t, err, "a sign-in the server never answered is no arrival")
	assert.ErrorContains(t, err, "answer")
}

func TestASignInThatLeavesAtItsSendWaitsForNoAnswer(t *testing.T) {
	codec := &protocol.Codec{}
	var sent bytes.Buffer
	stream := &deadlineLog{Reader: &bytes.Buffer{}, Writer: &sent}

	require.NoError(t, signIn(context.Background(), codec, stream, "host-1", loadOptions{leaveAtSend: true}))

	assert.Equal(t, protocol.MsgAgentRegister, readControl(t, codec, &sent).Type)
	assert.Empty(t, stream.deadlines, "nothing is read, so nothing is waited for")
}

func TestAMachineToldToComeBackLaterReportsTheWait(t *testing.T) {
	codec := &protocol.Codec{}
	var replies bytes.Buffer
	writeControl(t, codec, &replies, &protocol.ControlMessage{Type: protocol.MsgComeBackLater, RetryAfter: 7})

	err := signIn(context.Background(), codec, &pipeStream{r: &replies, w: &bytes.Buffer{}}, "host-1", loadOptions{})

	var told *toldToComeBack
	require.ErrorAs(t, err, &told, "a full line is an answer, not a silence")
	assert.Equal(t, 7*time.Second, told.wait)
}
