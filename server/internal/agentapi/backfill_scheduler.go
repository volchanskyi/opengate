package agentapi

import (
	"container/list"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// BackfillScheduler grants catch-up permission and admits each batch by the fill of the writer's
// catch-up queue, shared between customers; all methods are safe for concurrent use.
type BackfillScheduler struct {
	mu       sync.Mutex
	cfg      BackfillSchedulerConfig
	now      func() time.Time
	headroom func() float64
	fill     func() float64

	grants map[uuid.UUID]grant
	// soonest is at or before every grant's deadline, so nothing expires before it.
	soonest   time.Time
	inflight  map[uuid.UUID]*CatchUpBatch
	customers map[uuid.UUID]*customerTurns
	held      int
	throttled bool

	// drainRate is the measured bytes a second the queue writes, which sets a refusal's wait.
	drainRate    float64
	drained      int
	drainedSince time.Time
}

// grant is one machine's permission to catch up and the customer it catches up for.
type grant struct {
	deadline time.Time
	customer uuid.UUID
}

// NewBackfillScheduler builds a scheduler with an injectable clock, a headroom signal in 0..1 and
// the catch-up queue's fill; nil selects the wall clock, full headroom and an empty queue.
func NewBackfillScheduler(cfg BackfillSchedulerConfig, now func() time.Time, headroom, fill func() float64) *BackfillScheduler {
	if now == nil {
		now = time.Now
	}
	if headroom == nil {
		headroom = func() float64 { return 1.0 }
	}
	if fill == nil {
		fill = func() float64 { return 0 }
	}
	return &BackfillScheduler{
		cfg:          cfg,
		now:          now,
		headroom:     headroom,
		fill:         fill,
		grants:       make(map[uuid.UUID]grant),
		inflight:     make(map[uuid.UUID]*CatchUpBatch),
		customers:    make(map[uuid.UUID]*customerTurns),
		drainRate:    float64(cfg.BaseBudgetSamplesPerSec * encodedReadingBytes),
		drainedSince: now(),
	}
}

// RequestSlot grants a machine permission to send catch-up batches for its customer, or renews
// the grant it holds. The grant takes no place in the queue; Admit decides per batch.
func (s *BackfillScheduler) RequestSlot(machine, customer uuid.UUID, _ SlotRequest) BackfillDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)
	deadline := now.Add(s.cfg.GrantTTL)
	if held, ok := s.grants[machine]; ok && held.customer != customer {
		s.drop(machine)
	}
	turns, ok := s.customers[customer]
	if !ok {
		turns = &customerTurns{asked: list.New(), askedPos: map[uuid.UUID]*list.Element{}, seq: map[uuid.UUID]int{}}
		s.customers[customer] = turns
	}
	if _, renewing := s.grants[machine]; !renewing {
		turns.machines++
	}
	s.grants[machine] = grant{deadline: deadline, customer: customer}
	if s.soonest.IsZero() || deadline.Before(s.soonest) {
		s.soonest = deadline
	}
	return BackfillDecision{Rate: s.rate(), Deadline: deadline.Unix()}
}

// Admit decides one batch. An admitted batch holds its place until Complete; a nil scheduler
// admits nothing.
func (s *BackfillScheduler) Admit(b CatchUpBatch) BackfillAdmission {
	if s == nil {
		return BackfillAdmission{Reason: BackfillNoGrant, RetryAfter: uint32(minRetryAfter.Seconds())}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)
	g, ok := s.grants[b.Device]
	if !ok {
		return s.refuse(nil, b.Device, BackfillNoGrant)
	}
	if queued, ok := s.inflight[b.Device]; ok {
		if queued.Key == b.Key {
			queued.Answer = b.Answer
			return BackfillAdmission{Verdict: Joined}
		}
		return s.refuse(nil, b.Device, BackfillBusy)
	}
	s.updateMode()
	turns := s.customers[g.customer]
	if !s.fits(turns, b) {
		if s.throttled {
			turns.join(b.Device)
		}
		return s.refuse(turns, b.Device, BackfillBusy)
	}
	turns.leave(b.Device)
	if s.throttled {
		turns.credit -= b.Size
	}
	turns.held += b.Size
	s.held += b.Size
	b.Customer = g.customer
	s.inflight[b.Device] = &b
	return BackfillAdmission{Verdict: Admitted}
}

// fits reports whether a batch may go now: inside the queue and its customer's share, or when
// throttled, its customer's turn, overshoot repaid and machines in the order they asked.
func (s *BackfillScheduler) fits(turns *customerTurns, b CatchUpBatch) bool {
	if s.held+b.Size > s.cfg.Bound {
		return false
	}
	if !s.throttled {
		return turns.held == 0 || turns.held+b.Size <= s.share()
	}
	return turns.held == 0 && turns.credit >= 0 && turns.next(b.Device)
}

// Complete answers the batch a machine has in flight, on the connection of its newest copy,
// and gives its place back; each waiting customer earns an equal part of what was written.
func (s *BackfillScheduler) Complete(machine uuid.UUID, outcome telemetry.Outcome) {
	if s == nil {
		return
	}
	s.mu.Lock()
	b, ok := s.inflight[machine]
	if !ok {
		s.mu.Unlock()
		return
	}
	delete(s.inflight, machine)
	s.held -= b.Size
	if turns, ok := s.customers[b.Customer]; ok {
		turns.held -= b.Size
		s.forgetIdle(b.Customer, turns)
	}
	s.creditTurn(b.Size)
	s.measureDrain(b.Size)
	answer := b.Answer
	s.mu.Unlock()
	if answer != nil {
		answer(outcome)
	}
}

// Release drops a machine's grant and its place in its customer's rotation; a batch it has in
// flight is answered by Complete.
func (s *BackfillScheduler) Release(machine uuid.UUID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.drop(machine)
}

// ActiveCount is the number of batches in flight; a nil scheduler holds none.
func (s *BackfillScheduler) ActiveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.inflight)
}

// Throttled reports whether the queue gives each waiting customer one batch a turn.
func (s *BackfillScheduler) Throttled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.throttled
}

// updateMode turns throttling on at the high mark and off below the low mark.
func (s *BackfillScheduler) updateMode() {
	fill := s.fill()
	switch {
	case fill >= s.cfg.HighMark:
		s.throttled = true
	case fill < s.cfg.LowMark:
		s.throttled = false
	}
}

// share is each waiting customer's equal part of the queue.
func (s *BackfillScheduler) share() int {
	return s.cfg.Bound / max(1, len(s.customers))
}

// creditTurn gives every waiting customer an equal part of a written batch, up to one share.
func (s *BackfillScheduler) creditTurn(written int) {
	part := written / max(1, len(s.customers))
	share := s.share()
	for _, turns := range s.customers {
		turns.credit = min(turns.credit+part, share)
	}
}

// reap drops grants whose deadline has passed. It reads the grants only once the soonest deadline
// has come, since a renewal only moves a deadline later.
func (s *BackfillScheduler) reap(now time.Time) {
	if now.Before(s.soonest) {
		return
	}
	var soonest time.Time
	for machine, g := range s.grants {
		if !g.deadline.After(now) {
			s.drop(machine)
			continue
		}
		if soonest.IsZero() || g.deadline.Before(soonest) {
			soonest = g.deadline
		}
	}
	s.soonest = soonest
}

// drop removes a machine's grant, and its customer once no machine of it is catching up.
func (s *BackfillScheduler) drop(machine uuid.UUID) {
	g, ok := s.grants[machine]
	if !ok {
		return
	}
	delete(s.grants, machine)
	turns := s.customers[g.customer]
	turns.leave(machine)
	turns.machines--
	s.forgetIdle(g.customer, turns)
}

// forgetIdle removes a customer with no machine catching up and nothing in flight.
func (s *BackfillScheduler) forgetIdle(customer uuid.UUID, turns *customerTurns) {
	if turns.machines <= 0 && turns.held == 0 {
		delete(s.customers, customer)
	}
}
