package agentapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	promtestutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/alerts"
	"github.com/volchanskyi/opengate/server/internal/protocol"
	"github.com/volchanskyi/opengate/server/internal/settings"
)

// sentStream records what the server writes to the machine; answers are written from the
// persist goroutine, so reads and writes share a lock.
type sentStream struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *sentStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *sentStream) Read([]byte) (int, error) {
	return 0, errors.New("the machine sends nothing here")
}

// controls decodes every control frame written so far.
func (s *sentStream) controls() ([]*protocol.ControlMessage, error) {
	s.mu.Lock()
	written := bytes.NewReader(append([]byte(nil), s.buf.Bytes()...))
	s.mu.Unlock()
	codec := &protocol.Codec{}
	var msgs []*protocol.ControlMessage
	for written.Len() > 0 {
		frame, payload, err := codec.ReadFrame(written)
		if err != nil {
			return nil, err
		}
		if frame != protocol.FrameControl {
			return nil, fmt.Errorf("unexpected frame 0x%02x", frame)
		}
		msg, err := codec.DecodeControl(payload)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

// sent counts the control frames written so far; a frame that does not decode counts as one.
func (s *sentStream) sent() int {
	msgs, err := s.controls()
	if err != nil {
		return -1
	}
	return len(msgs)
}

// answered wires the fixture's connection to a recorded stream with the given capabilities.
func answered(f alertFixture, caps ...protocol.AgentCapability) *sentStream {
	stream := &sentStream{}
	f.conn.stream = stream
	f.conn.codec = &protocol.Codec{}
	f.conn.setMeta("linux", "amd64", "0.1.0", caps)
	return stream
}

// awaitAnswer waits for exactly one answer and returns it.
func awaitAnswer(t *testing.T, stream *sentStream) *protocol.ControlMessage {
	t.Helper()
	require.Eventually(t, func() bool { return stream.sent() != 0 },
		2*time.Second, 5*time.Millisecond, "the machine was never answered")
	assert.Never(t, func() bool { return stream.sent() != 1 },
		50*time.Millisecond, 5*time.Millisecond, "an offered alert gets exactly one answer")
	msgs, err := stream.controls()
	require.NoError(t, err)
	require.Len(t, msgs, 1)
	require.Equal(t, protocol.MsgAlertAnswer, msgs[0].Type)
	return msgs[0]
}

func TestEveryOfferedAlertGetsOneAnswerInTheRightWord(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()

	cases := []struct {
		name       string
		arrange    func(f alertFixture)
		change     func(*protocol.ControlMessage)
		payloadLen int
		noTenant   bool
		want       protocol.AlertAnswerReason
	}{
		{name: "stored", want: protocol.AlertAnswerStored},
		{
			name:    "stored by an earlier offer",
			arrange: func(f alertFixture) { f.store.outcome = alerts.Duplicate },
			want:    protocol.AlertAnswerStored,
		},
		{
			name:    "over the customer's hourly ceiling",
			arrange: func(f alertFixture) { f.store.outcome = alerts.CeilingSuppressed },
			want:    protocol.AlertAnswerRefusedForGood,
		},
		{
			name:       "past the payload bound",
			payloadLen: maxAlertPayloadBytes + 1,
			want:       protocol.AlertAnswerRefusedForGood,
		},
		{name: "unknown severity", change: severity("Panic"), want: protocol.AlertAnswerRefusedForGood},
		{
			name:   "incomplete identity",
			change: func(m *protocol.ControlMessage) { m.RuleVersion = 0 },
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name:   "a rule this build does not ship",
			change: func(m *protocol.ControlMessage) { m.RuleID = "contoso-custom-rule" },
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name: "a rule the customer stopped",
			arrange: func(f alertFixture) {
				f.conn.wantedEventRules = map[string]struct{}{"linux-hung-task": {}}
			},
			change: func(m *protocol.ControlMessage) { m.RuleID = "linux-oom-kill" },
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name:   "a time outside the window",
			change: stamped(now.Add(24 * time.Hour)),
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name:   "evidence in a codec this build cannot read",
			change: func(m *protocol.ControlMessage) { m.EvidenceCodec = "zstd-9" },
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name:   "unreadable evidence",
			change: func(m *protocol.ControlMessage) { m.Evidence = []byte{0xff, 0x00, 0x13} },
			want:   protocol.AlertAnswerRefusedForGood,
		},
		{
			name: "a customer that is not resolved yet",
			arrange: func(f alertFixture) {
				f.conn.settings = fixedReader{scope: settings.Scope{DeviceID: f.scope.DeviceID, TenantID: f.scope.TenantID}}
			},
			want: protocol.AlertAnswerTryAgain,
		},
		{
			name:    "a failed write",
			arrange: func(f alertFixture) { f.store.fail(errors.New("postgres unavailable")) },
			want:    protocol.AlertAnswerTryAgain,
		},
		{
			name:    "no store wired",
			arrange: func(f alertFixture) { f.conn.alertStore = nil },
			want:    protocol.AlertAnswerTryAgain,
		},
		{
			name: "every persist slot busy",
			arrange: func(f alertFixture) {
				f.conn.telemetrySlots = make(chan struct{}, telemetryConcurrentWrites)
				for range telemetryConcurrentWrites {
					f.conn.telemetrySlots <- struct{}{}
				}
			},
			want: protocol.AlertAnswerTryAgain,
		},
		{name: "no tenant on the connection", noTenant: true, want: protocol.AlertAnswerTryAgain},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := alertConn(t)
			stream := answered(f, protocol.CapAlerts, protocol.CapAlertAnswers)
			if tc.arrange != nil {
				tc.arrange(f)
			}
			msg := wellFormed(t)
			if tc.change != nil {
				tc.change(msg)
			}
			payloadLen := defaultAlertPayloadLen
			if tc.payloadLen != 0 {
				payloadLen = tc.payloadLen
			}
			ctx := f.ctx
			if tc.noTenant {
				ctx = context.Background()
			}

			require.NoError(t, f.conn.handleAgentAlert(ctx, msg, payloadLen))

			answer := awaitAnswer(t, stream)
			assert.Equal(t, msg.AlertID, answer.AlertID, "the answer names the offer it answers")
			assert.Equal(t, string(tc.want), answer.Reason)
			assert.InDelta(t, 1, promtestutil.ToFloat64(f.metrics.AlertAnswersTotal.WithLabelValues(
				alertAnswerLabel(tc.want))), 0, "each answer is counted under its word")
		})
	}
}

func TestAMachineThatDoesNotReadAnswersIsSentNone(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	stream := answered(f, protocol.CapAlerts)

	f.ingest(t, wellFormed(t))
	f.reachedStore(t, 1)
	f.ingest(t, broken(t, func(m *protocol.ControlMessage) { m.RuleVersion = 0 }))
	f.dropped(t, alertDropIdentityIncomplete)

	assert.Never(t, func() bool { return stream.sent() != 0 },
		100*time.Millisecond, 10*time.Millisecond, "an older agent is served exactly as before")
}

func TestAnAnswerToAMachineThatLeftIsDroppedAndTheWriteStillCompletes(t *testing.T) {
	t.Parallel()
	f := alertConn(t)
	stream := answered(f, protocol.CapAlerts, protocol.CapAlertAnswers)
	f.conn.markReleased()

	f.ingest(t, wellFormed(t))

	assert.Len(t, f.reachedStore(t, 1), 1, "the alert is stored though its machine has gone")
	assert.Never(t, func() bool { return stream.sent() != 0 },
		100*time.Millisecond, 10*time.Millisecond, "nothing is written to a released connection")
}

func TestAnAlertAnswerArrivingFromAMachineIsIgnored(t *testing.T) {
	t.Parallel()
	ac, buf := newTestAgentConn(t, [16]byte{1}, nil)
	writeControlMsg(t, ac.codec, buf, &protocol.ControlMessage{
		Type: protocol.MsgAlertAnswer, AlertID: "a1", Reason: string(protocol.AlertAnswerStored),
	})
	require.NoError(t, ac.handleControl(context.Background()),
		"a message only the server sends is ignored, not fatal")
}
