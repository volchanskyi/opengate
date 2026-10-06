package agentapi

import (
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BackfillScheduler admits reconnect-backfill drains under global and per-tenant caps, a
// load-adaptive rate budget and backoff aging; all methods are safe for concurrent use.
type BackfillScheduler struct {
	mu          sync.Mutex
	cfg         BackfillSchedulerConfig
	now         func() time.Time
	headroom    func() float64
	grants      map[uuid.UUID]backfillGrant
	tenantCount map[uuid.UUID]int
	// firstReq records when each deferred agent first asked, which drives backoff aging.
	firstReq map[uuid.UUID]time.Time
}

// BackfillSchedulerConfig tunes the admission control.
type BackfillSchedulerConfig struct {
	// MaxConcurrent is the global cap on simultaneously-draining agents.
	MaxConcurrent int
	// PerTenantMax caps simultaneously-draining agents within one tenant.
	PerTenantMax int
	// BaseBudgetSamplesPerSec is the total ingest budget at full headroom.
	BaseBudgetSamplesPerSec int
	// MinGrantRate / MaxGrantRate bound a single grant's samples/sec.
	MinGrantRate uint32
	MaxGrantRate uint32
	// GrantTTL is how long a grant stays valid; an expired grant frees its slot.
	GrantTTL time.Duration
	// DeferBackoff is the base retry interval for a deferred agent;
	// aging shortens it toward minRetryAfter.
	DeferBackoff time.Duration
}

type backfillGrant struct {
	tenant   uuid.UUID
	deadline time.Time
}

// SlotRequest carries the agent's backlog hints from RequestBackfillSlot.
type SlotRequest struct {
	PendingSamples uint64
	OldestTS       int64
}

// BackfillDecision is the scheduler's answer: either a grant (rate + deadline)
// or a deferral (retry-after seconds).
type BackfillDecision struct {
	Grant      bool
	Rate       uint32
	Deadline   int64
	RetryAfter uint32
}

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
		cfg:         cfg,
		now:         now,
		headroom:    headroom,
		grants:      make(map[uuid.UUID]backfillGrant),
		tenantCount: make(map[uuid.UUID]int),
		firstReq:    make(map[uuid.UUID]time.Time),
	}
}

// RequestSlot admits or defers a backfill drain for agentID in tenant, renewing a live grant.
func (s *BackfillScheduler) RequestSlot(agentID, tenant uuid.UUID, _ SlotRequest) BackfillDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)

	if g, ok := s.grants[agentID]; ok {
		g.deadline = now.Add(s.cfg.GrantTTL)
		s.grants[agentID] = g
		delete(s.firstReq, agentID)
		return s.grant(now)
	}

	if len(s.grants) >= s.cfg.MaxConcurrent || s.tenantCount[tenant] >= s.cfg.PerTenantMax {
		return s.deferSlot(agentID, now)
	}

	s.grants[agentID] = backfillGrant{tenant: tenant, deadline: now.Add(s.cfg.GrantTTL)}
	s.tenantCount[tenant]++
	delete(s.firstReq, agentID)
	return s.grant(now)
}

// Release frees agentID's slot; an unknown agent or a nil scheduler is a no-op.
func (s *BackfillScheduler) Release(agentID uuid.UUID) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.release(agentID)
	delete(s.firstReq, agentID)
}

// ActiveCount is the number of live (un-expired) grants.
func (s *BackfillScheduler) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reap(s.now())
	return len(s.grants)
}

// grant builds a grant decision at the current load-adaptive rate.
func (s *BackfillScheduler) grant(now time.Time) BackfillDecision {
	return BackfillDecision{
		Grant:    true,
		Rate:     s.rate(),
		Deadline: now.Add(s.cfg.GrantTTL).Unix(),
	}
}

// deferSlot builds a deferral whose backoff shrinks with how long the agent has waited.
func (s *BackfillScheduler) deferSlot(agentID uuid.UUID, now time.Time) BackfillDecision {
	first, ok := s.firstReq[agentID]
	if !ok {
		first = now
		s.firstReq[agentID] = now
	}
	waited := now.Sub(first)
	retry := max(s.cfg.DeferBackoff-waited, minRetryAfter)
	return BackfillDecision{RetryAfter: uint32(math.Ceil(retry.Seconds()))}
}

// rate is the equal per-slot share of the load-adaptive budget, clamped to the configured
// bounds; dividing by the global cap keeps the summed grants within the budget.
func (s *BackfillScheduler) rate() uint32 {
	h := min(1, max(0, s.headroom()))
	slots := max(1, s.cfg.MaxConcurrent)
	per := float64(s.cfg.BaseBudgetSamplesPerSec) * h / float64(slots)
	r := min(max(uint32(per), s.cfg.MinGrantRate), s.cfg.MaxGrantRate)
	return r
}

// reap drops grants whose deadline has passed, freeing their slots.
func (s *BackfillScheduler) reap(now time.Time) {
	for id, g := range s.grants {
		if !g.deadline.After(now) {
			s.release(id)
		}
	}
}

func (s *BackfillScheduler) release(agentID uuid.UUID) {
	g, ok := s.grants[agentID]
	if !ok {
		return
	}
	delete(s.grants, agentID)
	if s.tenantCount[g.tenant] <= 1 {
		delete(s.tenantCount, g.tenant)
	} else {
		s.tenantCount[g.tenant]--
	}
}
