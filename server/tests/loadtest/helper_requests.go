package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/stun/v3"
)

// helperRequestsPerAttempt is what one direct-link attempt asks the helper: two sides, each from
// up to three network interfaces.
const helperRequestsPerAttempt = 6

// retransmitSchedule is when an unanswered request is sent again: after rto, then doubling, for
// sends transmissions in all, waiting finalWaits times rto for the last answer.
type retransmitSchedule struct {
	rto        time.Duration
	sends      int
	finalWaits int
}

// rfc8489Schedule is a STUN client's own: seven sends from 500 ms, then sixteen times that.
var rfc8489Schedule = retransmitSchedule{rto: 500 * time.Millisecond, sends: 7, finalWaits: 16}

// HelperCount is what the run's sessions asked the server's helper and what it answered.
type HelperCount struct {
	Asked    int64 `json:"asked"`
	Answered int64 `json:"answered"`
}

// helperTally is the run-wide count every machine's sessions add to.
type helperTally struct {
	asked, answered atomic.Int64
	inFlight        sync.WaitGroup
}

// start asks helpers in the background on schedule, for as long as ctx lasts.
func (t *helperTally) start(ctx context.Context, helpers []*net.UDPAddr, schedule retransmitSchedule) {
	t.inFlight.Add(1)
	go func() {
		defer t.inFlight.Done()
		askHelpers(ctx, helpers, schedule, t)
	}()
}

// settle waits until every request started has been answered or has run out its schedule, so a
// request still waiting is not counted as one that went unanswered.
func (t *helperTally) settle() {
	t.inFlight.Wait()
}

// count is the tally for the bundle, or nil when no session asked, which differs from nought of
// nought.
func (t *helperTally) count() *HelperCount {
	if t == nil || t.asked.Load() == 0 {
		return nil
	}
	return &HelperCount{Asked: t.asked.Load(), Answered: t.answered.Load()}
}

// helperAddresses reads the helpers a session request names. They are wire data, so each is held
// to the load targets, as a relay URL is.
func helperAddresses(helpers []string) ([]*net.UDPAddr, error) {
	addrs := make([]*net.UDPAddr, 0, len(helpers))
	for _, helper := range helpers {
		hostPort, ok := strings.CutPrefix(helper, "stun:")
		if !ok {
			return nil, fmt.Errorf("helper %q is not a stun: URL", helper)
		}
		if err := CheckQUICAddress(hostPort); err != nil {
			return nil, err
		}
		addr, err := net.ResolveUDPAddr("udp", hostPort)
		if err != nil {
			return nil, fmt.Errorf("resolve helper %q: %w", helper, err)
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

// askHelpers sends an attempt's Binding requests to each helper, each from a socket of its own as
// a peer's interfaces are, and counts the answers once each is answered or out of schedule.
func askHelpers(ctx context.Context, helpers []*net.UDPAddr, schedule retransmitSchedule, tally *helperTally) {
	var wg sync.WaitGroup
	for _, helper := range helpers {
		for range helperRequestsPerAttempt {
			tally.asked.Add(1)
			wg.Go(func() {
				if askOnce(ctx, helper, schedule) {
					tally.answered.Add(1)
				}
			})
		}
	}
	wg.Wait()
}

// askOnce sends one Binding request on the schedule and reports whether its answer came back.
func askOnce(ctx context.Context, helper *net.UDPAddr, schedule retransmitSchedule) bool {
	conn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()
	// Closing the socket ends a read still waiting when the session does.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return false
	}
	buf := make([]byte, 1500)
	wait := schedule.rto
	for send := 1; send <= schedule.sends; send++ {
		if _, err := conn.WriteToUDP(request.Raw, helper); err != nil {
			return false
		}
		if send == schedule.sends {
			wait = schedule.rto * time.Duration(schedule.finalWaits)
		}
		answered, err := awaitAnswer(conn, buf, request.TransactionID, time.Now().Add(wait))
		if answered || err != nil {
			return answered
		}
		wait *= 2
	}
	return false
}

// awaitAnswer reads until the answer to transaction arrives or deadline passes; a read that
// fails for any other reason is returned.
func awaitAnswer(conn *net.UDPConn, buf []byte, transaction [stun.TransactionIDSize]byte, deadline time.Time) (bool, error) {
	if err := conn.SetReadDeadline(deadline); err != nil {
		return false, err
	}
	for {
		n, err := conn.Read(buf)
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		reply := stun.Message{Raw: buf[:n]}
		if reply.Decode() == nil && reply.Type == stun.BindingSuccess && reply.TransactionID == transaction {
			return true, nil
		}
	}
}
