package agentapi

import (
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BackfillScheduler grants catch-up permission and admits each catch-up batch under global and
// per-tenant caps on batches being written; all methods are safe for concurrent use.
type BackfillScheduler struct {
	mu       sync.Mutex
	cfg      BackfillSchedulerConfig
	now      func() time.Time
	headroom func() float64
	// grants maps each machine holding permission to its deadline; a grant holds no place.
	grants       map[uuid.UUID]time.Time
	places       int
	tenantPlaces map[uuid.UUID]int
	// firstDefer records when each deferred machine was first told to wait, which drives aging.
	firstDefer map[uuid.UUID]time.Time
}

// BackfillSchedulerConfig tunes the admission control.
type BackfillSchedulerConfig struct {
	// MaxConcurrent caps the catch-up batches being written at once across every machine.
	MaxConcurrent int
	// PerTenantMax caps the catch-up batches being written at once within one tenant.
	PerTenantMax int
	// BaseBudgetSamplesPerSec is the total ingest budget at full headroom.
	BaseBudgetSamplesPerSec int
	// MinGrantRate / MaxGrantRate bound a single grant's samples/sec.
	MinGrantRate uint32
	MaxGrantRate uint32
	// GrantTTL is how long a grant stays valid unless the machine asks again.
	GrantTTL time.Duration
	// DeferBackoff is the base retry interval for a deferred batch;
	// aging shortens it toward minRetryAfter.
	DeferBackoff time.Duration
}

// SlotRequest carries the agent's backlog hints from RequestBackfillSlot.
type SlotRequest struct {
	PendingSamples uint64
	OldestTS       int64
}

// BackfillDecision is the scheduler's grant: the pace a machine sends at and when the grant
// ends unless renewed.
type BackfillDecision struct {
	Rate     uint32
	Deadline int64
}

// BackfillAdmission is the scheduler's answer to one catch-up batch: a place to write it, or
// a deferral with the reason and the retry time.
type BackfillAdmission struct {
	Admitted   bool
	Reason     string
	RetryAfter uint32
}

// The reasons a catch-up batch is deferred before its write.
const (
	// BackfillNoGrant is a batch from a machine holding no live grant.
	BackfillNoGrant = "no_grant"
	// BackfillBusy is a batch arriving while the global or the tenant's places are all taken.
	BackfillBusy = "busy"
)

// minRetryAfter is the floor a deferred agent is ever asked to wait.
const minRetryAfter = time.Second

// DefaultBackfillSchedulerConfig returns the single-node defaults: a per-tenant cap below the
// global cap and a conservative ingest budget.
func DefaultBackfillSchedulerConfig() BackfillSchedulerConfig {
	return BackfillSchedulerConfig{
		MaxConcurrent:           8,
		PerTenantMax:            4,
		BaseBudgetSamplesPerSec: 20_000,
		MinGrantRate:            500,
		MaxGrantRate:            5_000,
		GrantTTL:                60 * time.Second,
		DeferBackoff:            30 * time.Second,
	}
}

// NewBackfillScheduler builds a scheduler with an injectable clock and headroom signal in 0..1;
// nil selects the wall clock and full headroom.
func NewBackfillScheduler(cfg BackfillSchedulerConfig, now func() time.Time, headroom func() float64) *BackfillScheduler {
	if now == nil {
		now = time.Now
	}
	if headroom == nil {
		headroom = func() float64 { return 1.0 }
	}
	return &BackfillScheduler{
		cfg:          cfg,
		now:          now,
		headroom:     headroom,
		grants:       make(map[uuid.UUID]time.Time),
		tenantPlaces: make(map[uuid.UUID]int),
		firstDefer:   make(map[uuid.UUID]time.Time),
	}
}

// RequestSlot grants agentID permission to send catch-up batches, or renews the grant it holds.
// The grant takes no place: a place is taken per batch, by Admit.
func (s *BackfillScheduler) RequestSlot(agentID, _ uuid.UUID, _ SlotRequest) BackfillDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)
	deadline := now.Add(s.cfg.GrantTTL)
	s.grants[agentID] = deadline
	return BackfillDecision{Rate: s.rate(), Deadline: deadline.Unix()}
}

// Admit takes a place for one of agentID's batches in tenant and returns the function that gives
// it back; a refused batch, or any batch on a nil scheduler, gets none.
func (s *BackfillScheduler) Admit(agentID, tenant uuid.UUID) (func(), BackfillAdmission) {
	if s == nil {
		return nil, BackfillAdmission{Reason: BackfillNoGrant, RetryAfter: uint32(minRetryAfter.Seconds())}
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)
	if _, ok := s.grants[agentID]; !ok {
		return nil, s.deferBatch(agentID, BackfillNoGrant, now)
	}
	if s.places >= s.cfg.MaxConcurrent || s.tenantPlaces[tenant] >= s.cfg.PerTenantMax {
		return nil, s.deferBatch(agentID, BackfillBusy, now)
	}
	s.places++
	s.tenantPlaces[tenant]++
	delete(s.firstDefer, agentID)
	var once sync.Once
	return func() { once.Do(func() { s.leave(tenant) }) }, BackfillAdmission{Admitted: true}
}

// Release drops agentID's grant; an unknown agent or a nil scheduler is a no-op. A place a batch
// holds is given back by that batch.
func (s *BackfillScheduler) Release(agentID uuid.UUID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.grants, agentID)
	delete(s.firstDefer, agentID)
}

// ActiveCount is the number of places held by batches being written; a nil scheduler holds none.
func (s *BackfillScheduler) ActiveCount() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.places
}

// leave gives back one place in tenant.
func (s *BackfillScheduler) leave(tenant uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.places--
	if s.tenantPlaces[tenant] <= 1 {
		delete(s.tenantPlaces, tenant)
	} else {
		s.tenantPlaces[tenant]--
	}
}

// deferBatch builds a deferral whose backoff shrinks with how long the agent has waited.
func (s *BackfillScheduler) deferBatch(agentID uuid.UUID, reason string, now time.Time) BackfillAdmission {
	first, ok := s.firstDefer[agentID]
	if !ok {
		first = now
		s.firstDefer[agentID] = now
	}
	waited := now.Sub(first)
	retry := max(s.cfg.DeferBackoff-waited, minRetryAfter)
	return BackfillAdmission{Reason: reason, RetryAfter: uint32(math.Ceil(retry.Seconds()))}
}

// rate is the equal per-place share of the load-adaptive budget, clamped to the configured
// bounds; dividing by the global cap keeps the summed grants within the budget.
func (s *BackfillScheduler) rate() uint32 {
	h := min(1, max(0, s.headroom()))
	slots := max(1, s.cfg.MaxConcurrent)
	per := float64(s.cfg.BaseBudgetSamplesPerSec) * h / float64(slots)
	return min(max(uint32(per), s.cfg.MinGrantRate), s.cfg.MaxGrantRate)
}

// reap drops grants whose deadline has passed.
func (s *BackfillScheduler) reap(now time.Time) {
	for id, deadline := range s.grants {
		if !deadline.After(now) {
			delete(s.grants, id)
		}
	}
}
