package agentapi

import (
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// BackfillScheduler admits reconnect-backfill drains across all connected
// agents. It is the server-coordinated admission control from the WS-15 plan:
//
//   - a global concurrency cap so backfill never stampedes the single node;
//   - a per-tenant concurrency cap that is the weighted max-min fair-share lever
//     — no org can grab more than its share of the global slots and starve
//     another tenant;
//   - a load-adaptive ingest budget (base samples/sec scaled by live headroom)
//     spread as an equal rate slice per admitted slot, so the sum of granted
//     rates never oversubscribes the budget and every rate shrinks as the node
//     comes under live pressure;
//   - grant/defer with aging: a deferred agent's backoff shortens the longer it
//     has waited, so a busy node cannot starve any agent indefinitely.
//
// Backfill always yields to live telemetry and control: those are handled on
// their own paths and never pass through the scheduler, so admitting or
// deferring a backfill drain cannot delay them. The scheduler is in-memory and
// single-replica today; a multi-replica rollout would gate on a shared VM
// ingest-rate signal instead of these per-replica counters.
//
// All methods are safe for concurrent use.
type BackfillScheduler struct {
	mu       sync.Mutex
	cfg      BackfillSchedulerConfig
	now      func() time.Time
	headroom func() float64
	grants   map[uuid.UUID]backfillGrant
	orgCount map[uuid.UUID]int
	// firstReq records when a currently-deferred agent first asked, so aging can
	// shorten its backoff. Cleared when the agent is granted or released.
	firstReq map[uuid.UUID]time.Time
}

// BackfillSchedulerConfig tunes the admission control.
type BackfillSchedulerConfig struct {
	// MaxConcurrent is the global cap on simultaneously-draining agents.
	MaxConcurrent int
	// PerTenantMax caps simultaneously-draining agents within one org.
	PerTenantMax int
	// BaseBudgetSamplesPerSec is the total ingest budget at full headroom.
	BaseBudgetSamplesPerSec int
	// MinGrantRate / MaxGrantRate bound a single grant's samples/sec.
	MinGrantRate uint32
	MaxGrantRate uint32
	// GrantTTL is how long a grant is valid before it must be re-requested; an
	// agent that drops without releasing frees its slot when its grant expires.
	GrantTTL time.Duration
	// DeferBackoff is the base retry interval handed to a deferred agent (which
	// adds its own jitter); aging reduces it toward MinRetry.
	DeferBackoff time.Duration
}

type backfillGrant struct {
	org      uuid.UUID
	deadline time.Time
}

// SlotRequest carries the agent's backlog hints from RequestBackfillSlot. They
// let a future scheduler bias priority by backlog size/age; admission today is
// by caps, budget, and aging.
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

// DefaultBackfillSchedulerConfig returns the single-node production defaults:
// a bounded number of concurrent drains, a per-tenant cap well below the global
// cap so one org cannot monopolize the node, and a conservative ingest budget.
// Backfill has no urgency (local data is durable), so the scheduler can be
// stingy. The live-headroom signal that shrinks the budget under load is wired
// separately; until then it runs at full headroom.
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

// NewBackfillScheduler builds a scheduler with an injectable clock and live-
// headroom signal (0..1). Passing nil for either uses safe defaults
// (wall-clock / full headroom).
func NewBackfillScheduler(cfg BackfillSchedulerConfig, now func() time.Time, headroom func() float64) *BackfillScheduler {
	if now == nil {
		now = time.Now
	}
	if headroom == nil {
		headroom = func() float64 { return 1.0 }
	}
	return &BackfillScheduler{
		cfg:      cfg,
		now:      now,
		headroom: headroom,
		grants:   make(map[uuid.UUID]backfillGrant),
		orgCount: make(map[uuid.UUID]int),
		firstReq: make(map[uuid.UUID]time.Time),
	}
}

// RequestSlot admits or defers a backfill drain for agentID in org. A re-request
// from an agent that already holds a live grant renews it in place.
func (s *BackfillScheduler) RequestSlot(agentID, org uuid.UUID, _ SlotRequest) BackfillDecision {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	s.reap(now)

	// Idempotent renew: an agent re-requesting within its TTL keeps its slot.
	if g, ok := s.grants[agentID]; ok {
		g.deadline = now.Add(s.cfg.GrantTTL)
		s.grants[agentID] = g
		delete(s.firstReq, agentID)
		return s.grant(now)
	}

	if len(s.grants) >= s.cfg.MaxConcurrent || s.orgCount[org] >= s.cfg.PerTenantMax {
		return s.deferSlot(agentID, now)
	}

	s.grants[agentID] = backfillGrant{org: org, deadline: now.Add(s.cfg.GrantTTL)}
	s.orgCount[org]++
	delete(s.firstReq, agentID)
	return s.grant(now)
}

// Release frees agentID's slot (drain complete or connection closed). Unknown
// agents — and a nil scheduler (a connection wired without one) — are a no-op.
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

// deferSlot builds a deferral decision, recording the agent's first-wait time
// and shortening the backoff by how long it has already waited (aging).
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

// rate is the equal per-slot share of the load-adaptive budget, clamped to the
// configured bounds. Dividing by the global cap guarantees the sum of all live
// grant rates never oversubscribes the budget.
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
	if s.orgCount[g.org] <= 1 {
		delete(s.orgCount, g.org)
	} else {
		s.orgCount[g.org]--
	}
}
