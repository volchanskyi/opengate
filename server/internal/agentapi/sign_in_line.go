package agentapi

import (
	"container/list"
	"context"
	"crypto/rand"
	"errors"
	"math"
	"math/big"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"

	appmetrics "github.com/volchanskyi/opengate/server/internal/metrics"
)

// errComeBackLater is a sign-in the line has no room for; the machine is told when to dial again.
var errComeBackLater = errors.New("the sign-in line is full")

const (
	// signInsInProgressPerProcessor is how many sign-ins run at once per processor of allowance:
	// two at a quarter processor read fastest on the rehearsal, and eight read slowest.
	signInsInProgressPerProcessor = 8
	// signInsPerSecondPerProcessor is the promised rate, 100 a second on a quarter processor, which
	// the line counts on until it has measured a pace of its own.
	signInsPerSecondPerProcessor = 400
	// signInLineSeconds is how much work the line holds before it turns machines away.
	signInLineSeconds = 30
	// comeBackWithinSeconds bounds the random wait a turned-away machine is given.
	comeBackWithinSeconds = 30
)

// signInLineSize is how many sign-ins run at once, and the rate promised, for a processor share.
func signInLineSize(processorShare float64) (inProgress int, promisedPerSecond float64) {
	inProgress = max(1, int(math.Ceil(signInsInProgressPerProcessor*processorShare)))
	return inProgress, signInsPerSecondPerProcessor * processorShare
}

// comeBackWait is a whole number of seconds from nought to comeBackWithinSeconds, drawn evenly,
// so a turned-away crowd returns spread over the line's length; a failed draw waits the most.
func comeBackWait() uint32 {
	n, err := rand.Int(rand.Reader, big.NewInt(comeBackWithinSeconds+1))
	if err != nil {
		return comeBackWithinSeconds
	}
	return uint32(n.Int64())
}

// newSignInLineFor sizes a line for the processor share, every processor when it is zero, and
// publishes how many wait on the metrics when there are any.
func newSignInLineFor(processorShare float64, metrics *appmetrics.Metrics) *signInLine {
	if processorShare <= 0 {
		processorShare = float64(runtime.NumCPU())
	}
	inProgress, promised := signInLineSize(processorShare)
	line := newSignInLine(inProgress, promised, time.Now)
	if metrics != nil {
		line.waitingGauge = metrics.SignInLineWaiting
	}
	return line
}

// lineGauge is the published count of sign-ins waiting.
type lineGauge interface{ Add(float64) }

// signInLine runs a set number of sign-ins at once; the rest wait connected, customers taking
// turns in arrival order, up to 30 s of the pace the line keeps.
type signInLine struct {
	mu   sync.Mutex
	free int
	held int
	pace pace
	now  func() time.Time
	// turns holds each customer's waiting sign-ins; ring orders the customers with any waiting.
	turns map[uuid.UUID]*customerTurn
	ring  *list.List
	// waitingGauge follows held when set.
	waitingGauge lineGauge
}

// customerTurn is one customer's waiting sign-ins and its place in the ring.
type customerTurn struct {
	customer uuid.UUID
	queue    *list.List
	place    *list.Element
}

// lineSeat is one waiting sign-in; its answer says it got a slot or was turned away.
type lineSeat struct {
	turn   *customerTurn
	place  *list.Element
	answer chan error
}

func newSignInLine(inProgress int, promisedPerSecond float64, now func() time.Time) *signInLine {
	return &signInLine{
		free:  inProgress,
		pace:  pace{perSecond: promisedPerSecond},
		now:   now,
		turns: make(map[uuid.UUID]*customerTurn),
		ring:  list.New(),
	}
}

// capacity is how many may wait: 30 s of the pace, never below one.
func (l *signInLine) capacity() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.room()
}

func (l *signInLine) room() int {
	return max(1, int(math.Round(signInLineSeconds*l.pace.perSecond)))
}

// waiting is how many sign-ins are in the line.
func (l *signInLine) waiting() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held
}

// enter takes a slot for a sign-in of the customer, waiting its turn, and returns how to give it
// back. A full line turns away the newest of the customer holding the most of it.
func (l *signInLine) enter(ctx context.Context, customer uuid.UUID) (func(), error) {
	l.mu.Lock()
	if l.free > 0 && l.held == 0 {
		l.free--
		l.mu.Unlock()
		return l.leaver(), nil
	}
	if l.held >= l.room() && !l.makeRoomFor(customer) {
		l.mu.Unlock()
		return nil, errComeBackLater
	}
	seat := l.seat(customer)
	l.mu.Unlock()

	select {
	case err := <-seat.answer:
		if err != nil {
			return nil, err
		}
		return l.leaver(), nil
	case <-ctx.Done():
		l.mu.Lock()
		defer l.mu.Unlock()
		if seat.place != nil {
			l.unseat(seat)
			return nil, ctx.Err()
		}
		// The slot or the refusal arrived as the machine hung up.
		if err := <-seat.answer; err == nil {
			l.handOn()
		}
		return nil, ctx.Err()
	}
}

// makeRoomFor turns away the newest sign-in of the customer that would hold the most of the line
// once the newcomer joined, and reports whether that left room; ties go against the newcomer.
func (l *signInLine) makeRoomFor(customer uuid.UUID) bool {
	own := 1
	if turn, ok := l.turns[customer]; ok {
		own += turn.queue.Len()
	}
	var longest *customerTurn
	for _, turn := range l.turns {
		if longest == nil || turn.queue.Len() > longest.queue.Len() {
			longest = turn
		}
	}
	if longest == nil || longest.customer == customer || longest.queue.Len() <= own {
		return false
	}
	newest := longest.queue.Back().Value.(*lineSeat)
	l.unseat(newest)
	newest.answer <- errComeBackLater
	return true
}

// seat puts a sign-in at the back of its customer's queue, adding the customer to the ring.
func (l *signInLine) seat(customer uuid.UUID) *lineSeat {
	turn, ok := l.turns[customer]
	if !ok {
		turn = &customerTurn{customer: customer, queue: list.New()}
		turn.place = l.ring.PushBack(turn)
		l.turns[customer] = turn
	}
	seat := &lineSeat{turn: turn, answer: make(chan error, 1)}
	seat.place = turn.queue.PushBack(seat)
	l.held++
	l.publish(1)
	return seat
}

// unseat takes a sign-in out of the line, and its customer out of the ring once none wait.
func (l *signInLine) unseat(seat *lineSeat) {
	turn := seat.turn
	turn.queue.Remove(seat.place)
	seat.place = nil
	l.held--
	l.publish(-1)
	if turn.queue.Len() == 0 {
		l.ring.Remove(turn.place)
		delete(l.turns, turn.customer)
	}
}

// publish moves the waiting gauge by delta.
func (l *signInLine) publish(delta float64) {
	if l.waitingGauge != nil {
		l.waitingGauge.Add(delta)
	}
}

// leaver gives a slot back once, however often it is called.
func (l *signInLine) leaver() func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.handOn()
		})
	}
}

// handOn passes a freed slot to the first waiting sign-in of the customer whose turn is next,
// which then goes to the back of the ring; with nobody waiting the slot is free.
func (l *signInLine) handOn() {
	front := l.ring.Front()
	if front == nil {
		l.free++
		return
	}
	turn := front.Value.(*customerTurn)
	l.ring.MoveToBack(front)
	next := turn.queue.Front().Value.(*lineSeat)
	l.unseat(next)
	l.pace.note(l.now())
	next.answer <- nil
}

const (
	// paceSamples is how many hand-offs the pace remembers.
	paceSamples = 64
	// paceWindow and paceMinimum are how recent, and how many, the hand-offs a new pace is read from.
	paceWindow  = 10 * time.Second
	paceMinimum = 8
)

// pace is the sign-ins finished a second, read from slots handed straight to a waiting machine;
// short of recent hand-offs the last pace stands, the promised rate before any.
type pace struct {
	stamps    [paceSamples]time.Time
	count     int
	next      int
	perSecond float64
}

func (p *pace) note(at time.Time) {
	p.stamps[p.next] = at
	p.next = (p.next + 1) % paceSamples
	p.count = min(p.count+1, paceSamples)
	oldest, recent := at, 0
	for _, stamp := range p.stamps[:p.count] {
		if at.Sub(stamp) <= paceWindow {
			recent++
			if stamp.Before(oldest) {
				oldest = stamp
			}
		}
	}
	if recent >= paceMinimum && at.After(oldest) {
		p.perSecond = float64(recent-1) / at.Sub(oldest).Seconds()
	}
}
