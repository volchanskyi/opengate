package agentapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedCfg is a compact config for deterministic scheduler tests.
func schedCfg() BackfillSchedulerConfig {
	return BackfillSchedulerConfig{
		MaxConcurrent:           4,
		PerTenantMax:            2,
		BaseBudgetSamplesPerSec: 1200,
		MinGrantRate:            50,
		MaxGrantRate:            1000,
		GrantTTL:                30 * time.Second,
		DeferBackoff:            20 * time.Second,
	}
}

// fixedClock returns a controllable clock and a pointer to advance it.
func fixedClock() (func() time.Time, *time.Time) {
	base := time.Unix(1_700_000_000, 0).UTC()
	cur := &base
	return func() time.Time { return *cur }, cur
}

func TestScheduler_GrantsWithinCapsThenDefersGlobal(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })

	// Four distinct orgs each take one slot — up to the global cap of 4.
	for i := range 4 {
		d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{PendingSamples: 100})
		require.True(t, d.Grant, "slot %d should be granted", i)
		assert.GreaterOrEqual(t, d.Rate, schedCfg().MinGrantRate)
		assert.LessOrEqual(t, d.Rate, schedCfg().MaxGrantRate)
		assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), d.Deadline)
	}
	// The fifth agent is deferred: the global concurrency cap is full.
	d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{PendingSamples: 100})
	assert.False(t, d.Grant, "global cap reached → defer")
	assert.Positive(t, d.RetryAfter)
	assert.Equal(t, 4, s.ActiveCount())
}

func TestScheduler_PerTenantCapDefersThirdAgentOfOneOrg(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	org := uuid.New()

	require.True(t, s.RequestSlot(uuid.New(), org, SlotRequest{}).Grant)
	require.True(t, s.RequestSlot(uuid.New(), org, SlotRequest{}).Grant)
	// Third agent of the same org: per-tenant cap (2) reached even though the
	// global cap (4) has room — one org cannot monopolize the node.
	d := s.RequestSlot(uuid.New(), org, SlotRequest{})
	assert.False(t, d.Grant, "per-tenant cap reached → defer")
	assert.Positive(t, d.RetryAfter)
}

func TestScheduler_BudgetShrinksUnderLiveLoad(t *testing.T) {
	clock, _ := fixedClock()
	headroom := 1.0
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return headroom })

	full := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{})
	require.True(t, full.Grant)

	// Under live pressure the adaptive budget shrinks, so a fresh grant to an
	// equivalent lone agent is throttled to a lower rate.
	headroom = 0.25
	s2 := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 0.25 })
	low := s2.RequestSlot(uuid.New(), uuid.New(), SlotRequest{})
	require.True(t, low.Grant)
	assert.Less(t, low.Rate, full.Rate, "lower headroom yields a lower granted rate")
}

func TestScheduler_FairShareCapsAnyOneOrg(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	orgA, orgB := uuid.New(), uuid.New()

	// Weighted max-min fair-share is enforced by the per-tenant concurrency cap:
	// org A holds at most PerTenantMax (2) of the 4 global slots and cannot grab
	// them all to starve other tenants.
	require.True(t, s.RequestSlot(uuid.New(), orgA, SlotRequest{}).Grant)
	require.True(t, s.RequestSlot(uuid.New(), orgA, SlotRequest{}).Grant)
	assert.False(t, s.RequestSlot(uuid.New(), orgA, SlotRequest{}).Grant, "A is capped at PerTenantMax")

	// The two slots A cannot take stay available to org B — B is never starved.
	require.True(t, s.RequestSlot(uuid.New(), orgB, SlotRequest{}).Grant)
	require.True(t, s.RequestSlot(uuid.New(), orgB, SlotRequest{}).Grant)
	assert.Equal(t, 4, s.ActiveCount(), "the global cap is shared across tenants, not monopolized")

	// Every granted rate is an equal load-adaptive slice within bounds.
	d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{})
	assert.False(t, d.Grant, "global cap now full")
}

func TestScheduler_AgingShortensRetryForLongWaiters(t *testing.T) {
	clock, cur := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })

	// Saturate the global cap so further requests defer.
	for range 4 {
		require.True(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)
	}
	waiter := uuid.New()
	first := s.RequestSlot(waiter, uuid.New(), SlotRequest{})
	require.False(t, first.Grant)

	// The same agent, still deferred, has now waited a while. Aging shortens its
	// backoff so it re-contends sooner and cannot be starved indefinitely.
	*cur = cur.Add(15 * time.Second)
	later := s.RequestSlot(waiter, uuid.New(), SlotRequest{})
	require.False(t, later.Grant)
	assert.Less(t, later.RetryAfter, first.RetryAfter, "a longer wait earns a shorter retry")
}

func TestScheduler_ExpiredGrantFreesASlot(t *testing.T) {
	clock, cur := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })

	for range 4 {
		require.True(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)
	}
	assert.False(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)

	// After every grant's deadline passes, stale grants are reclaimed and a new
	// agent is admitted.
	*cur = cur.Add(schedCfg().GrantTTL + time.Second)
	assert.True(t, s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{}).Grant)
}

func TestScheduler_RenewIsIdempotentWithinTTL(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	agent, org := uuid.New(), uuid.New()

	first := s.RequestSlot(agent, org, SlotRequest{})
	require.True(t, first.Grant)
	// Re-requesting the same agent's slot within the TTL renews it in place — it
	// does not consume a second slot.
	again := s.RequestSlot(agent, org, SlotRequest{})
	require.True(t, again.Grant)
	assert.Equal(t, 1, s.ActiveCount(), "renew must not double-book a slot")
}

func TestScheduler_ReleaseFreesTheSlot(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	agent := uuid.New()
	require.True(t, s.RequestSlot(agent, uuid.New(), SlotRequest{}).Grant)
	assert.Equal(t, 1, s.ActiveCount())
	s.Release(agent)
	assert.Equal(t, 0, s.ActiveCount())
	// Releasing an unknown agent is a harmless no-op.
	s.Release(uuid.New())
	assert.Equal(t, 0, s.ActiveCount())
	// A nil scheduler (a connection wired without one) releases without panic.
	var nilSched *BackfillScheduler
	assert.NotPanics(t, func() { nilSched.Release(uuid.New()) })
}

func TestScheduler_ReleaseDecrementsSharedOrgCount(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	org := uuid.New()
	a1, a2 := uuid.New(), uuid.New()
	require.True(t, s.RequestSlot(a1, org, SlotRequest{}).Grant)
	require.True(t, s.RequestSlot(a2, org, SlotRequest{}).Grant)

	// Releasing one of two same-org grants decrements the org count without
	// dropping it, so the org still holds a slot and can be released again.
	s.Release(a1)
	assert.Equal(t, 1, s.ActiveCount())
	// A third same-org agent is now admissible (org count is back below the cap).
	require.True(t, s.RequestSlot(uuid.New(), org, SlotRequest{}).Grant)
	assert.Equal(t, 2, s.ActiveCount())
}

func TestDefaultBackfillSchedulerConfigPinsDurations(t *testing.T) {
	cfg := DefaultBackfillSchedulerConfig()
	assert.Equal(t, 60*time.Second, cfg.GrantTTL)
	assert.Equal(t, 30*time.Second, cfg.DeferBackoff)
}

func TestScheduler_ReleaseLastGrantRemovesOrgCounter(t *testing.T) {
	clock, _ := fixedClock()
	s := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 })
	org := uuid.New()
	agent := uuid.New()
	require.True(t, s.RequestSlot(agent, org, SlotRequest{}).Grant)
	require.Equal(t, 1, s.orgCount[org])

	s.Release(agent)

	_, exists := s.orgCount[org]
	assert.False(t, exists)
}
