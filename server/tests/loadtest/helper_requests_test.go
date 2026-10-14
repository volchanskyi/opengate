package main

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/volchanskyi/opengate/server/internal/stunhelper"
)

// quickSchedule keeps a test's unanswered wait short; the shape is RFC 8489's.
var quickSchedule = retransmitSchedule{rto: 20 * time.Millisecond, sends: 3, finalWaits: 2}

// fakeHelper is a helper on loopback answering through the server's own responder. It drops the
// first packet from each port it is told to, and records every port it heard from.
type fakeHelper struct {
	conn     *net.UDPConn
	answer   bool
	dropOnce bool

	mu      sync.Mutex
	packets int
	ports   map[uint16]int
}

func startFakeHelper(t *testing.T, answer, dropOnce bool) *fakeHelper {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	h := &fakeHelper{conn: conn, answer: answer, dropOnce: dropOnce, ports: map[uint16]int{}}
	responder := stunhelper.New(stunhelper.DefaultLimits)
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFromUDPAddrPort(buf)
			if err != nil {
				return
			}
			h.mu.Lock()
			h.packets++
			h.ports[from.Port()]++
			first := h.ports[from.Port()] == 1
			h.mu.Unlock()
			if !h.answer || (h.dropOnce && first) {
				continue
			}
			if reply, _ := responder.Answer(buf[:n], from, time.Now()); reply != nil {
				_, _ = conn.WriteToUDPAddrPort(reply, from)
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return h
}

func (h *fakeHelper) addr() *net.UDPAddr { return h.conn.LocalAddr().(*net.UDPAddr) }

func (h *fakeHelper) heard() (packets, ports int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.packets, len(h.ports)
}

func TestAnAttemptsHelperRequestsAreAnsweredEachFromASocketOfItsOwn(t *testing.T) {
	helper := startFakeHelper(t, true, false)
	tally := &helperTally{}

	askHelpers(t.Context(), []*net.UDPAddr{helper.addr()}, quickSchedule, tally)

	assert.Equal(t, &HelperCount{Asked: helperRequestsPerAttempt, Answered: helperRequestsPerAttempt}, tally.count())
	packets, ports := helper.heard()
	assert.Equal(t, helperRequestsPerAttempt, packets)
	assert.Equal(t, helperRequestsPerAttempt, ports, "each request leaves from its own socket, as a peer's interfaces do")
}

func TestARequestTheHelperDroppedIsAskedAgain(t *testing.T) {
	helper := startFakeHelper(t, true, true)
	tally := &helperTally{}

	askHelpers(t.Context(), []*net.UDPAddr{helper.addr()}, quickSchedule, tally)

	assert.Equal(t, &HelperCount{Asked: helperRequestsPerAttempt, Answered: helperRequestsPerAttempt}, tally.count())
	packets, _ := helper.heard()
	assert.Equal(t, 2*helperRequestsPerAttempt, packets, "every dropped request was sent once more")
}

func TestARequestNobodyAnswersIsCountedUnanswered(t *testing.T) {
	helper := startFakeHelper(t, false, false)
	tally := &helperTally{}

	askHelpers(t.Context(), []*net.UDPAddr{helper.addr()}, quickSchedule, tally)

	assert.Equal(t, &HelperCount{Asked: helperRequestsPerAttempt, Answered: 0}, tally.count())
	packets, _ := helper.heard()
	assert.Equal(t, quickSchedule.sends*helperRequestsPerAttempt, packets, "each request is sent on the whole schedule")
}

func TestAskingStopsWhenTheSessionEnds(t *testing.T) {
	helper := startFakeHelper(t, false, false)
	tally := &helperTally{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	started := time.Now()
	askHelpers(ctx, []*net.UDPAddr{helper.addr()}, retransmitSchedule{rto: time.Second, sends: 7, finalWaits: 16}, tally)

	assert.Less(t, time.Since(started), time.Second)
	assert.Equal(t, int64(0), tally.count().Answered)
}

func TestARunThatAskedNothingCarriesNoHelperCount(t *testing.T) {
	assert.Nil(t, (&helperTally{}).count())
	assert.Nil(t, (*helperTally)(nil).count())
}

func TestTheHelpersASessionNamesAreHeldToTheLoadTargets(t *testing.T) {
	addrs, err := helperAddresses([]string{"stun:127.0.0.1:9090", "stun:localhost:3478"})
	require.NoError(t, err)
	require.Len(t, addrs, 2)
	assert.Equal(t, 9090, addrs[0].Port)

	for _, refused := range []string{
		"stun:quic.opengate.cloudisland.net:9090",
		"stun:203.0.113.7:9090",
		"turn:127.0.0.1:9090",
		"stun:127.0.0.1",
	} {
		_, err := helperAddresses([]string{refused})
		assert.Error(t, err, "%s is refused", refused)
	}
}

func TestEveryHelperRequestASessionMakesMustBeAnswered(t *testing.T) {
	all := classify(func(in *RunInputs) { in.Helper = &HelperCount{Asked: 120, Answered: 120} })
	assert.Equal(t, ResultValid, all.Result)

	short := classify(func(in *RunInputs) { in.Helper = &HelperCount{Asked: 120, Answered: 119} })
	assert.Equal(t, ResultFailed, short.Result)
	require.Len(t, short.Reasons, 1)
	assert.Contains(t, short.Reasons[0], "119 of 120")

	assert.Equal(t, ResultValid, classify(nil).Result, "a run whose sessions named no helper is judged on the rest")
}

func TestABundleRefusesMoreHelperAnswersThanRequests(t *testing.T) {
	b := completeBundle()
	b.Helper = &HelperCount{Asked: 6, Answered: 7}
	require.Error(t, b.Validate())

	b.Helper = &HelperCount{Asked: 6, Answered: 6}
	require.NoError(t, b.Validate())
}

func TestTheCountWaitsForRequestsStillOnTheirSchedule(t *testing.T) {
	answering := startFakeHelper(t, true, true)
	silent := startFakeHelper(t, false, false)
	tally := &helperTally{}

	tally.start(t.Context(), []*net.UDPAddr{answering.addr()}, quickSchedule)
	tally.start(t.Context(), []*net.UDPAddr{silent.addr()}, quickSchedule)
	tally.settle()

	assert.Equal(t, &HelperCount{Asked: 2 * helperRequestsPerAttempt, Answered: helperRequestsPerAttempt}, tally.count())
}
