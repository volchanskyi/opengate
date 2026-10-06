package agentapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schedCfg returns a small deterministic scheduler config.
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

// fullHeadroomScheduler builds a scheduler on the fixed clock with all headroom free.
func fullHeadroomScheduler() (*BackfillScheduler, func() time.Time, *time.Time) {
	clock, cur := fixedClock()
	return NewBackfillScheduler(schedCfg(), clock, func() float64 { return 1.0 }), clock, cur
}

// requestFor asks for a slot for a fresh agent of the tenant.
func requestFor(s *BackfillScheduler, tenant uuid.UUID) BackfillDecision {
	return s.RequestSlot(uuid.New(), tenant, SlotRequest{})
}

// grantFor requires a fresh agent of the tenant to be granted a slot.
func grantFor(t *testing.T, s *BackfillScheduler, tenant uuid.UUID) {
	t.Helper()
	require.True(t, requestFor(s, tenant).Grant)
}

// fillGlobalCap grants one slot to each of four distinct tenants.
func fillGlobalCap(t *testing.T, s *BackfillScheduler) {
	t.Helper()
	for range 4 {
		grantFor(t, s, uuid.New())
	}
}

func TestScheduler_GrantsWithinCapsThenDefersGlobal(t *testing.T) {
	s, clock, _ := fullHeadroomScheduler()

	for i := range 4 {
		d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{PendingSamples: 100})
		require.True(t, d.Grant, "slot %d should be granted", i)
		assert.GreaterOrEqual(t, d.Rate, schedCfg().MinGrantRate)
		assert.LessOrEqual(t, d.Rate, schedCfg().MaxGrantRate)
		assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), d.Deadline)
	}
	d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{PendingSamples: 100})
	assert.False(t, d.Grant, "global cap reached → defer")
	assert.Positive(t, d.RetryAfter)
	assert.Equal(t, 4, s.ActiveCount())
}

func TestScheduler_PerTenantCapDefersThirdAgentOfOneTenant(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()

	grantFor(t, s, tenant)
	grantFor(t, s, tenant)
	d := requestFor(s, tenant)
	assert.False(t, d.Grant, "per-tenant cap reached → defer")
	assert.Positive(t, d.RetryAfter)
}

func TestScheduler_BudgetShrinksUnderLiveLoad(t *testing.T) {
	s, clock, _ := fullHeadroomScheduler()

	full := requestFor(s, uuid.New())
	require.True(t, full.Grant)

	s2 := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 0.25 })
	low := requestFor(s2, uuid.New())
	require.True(t, low.Grant)
	assert.Less(t, low.Rate, full.Rate, "lower headroom yields a lower granted rate")
}

func TestScheduler_FairShareCapsAnyOneTenant(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenantA, tenantB := uuid.New(), uuid.New()

	grantFor(t, s, tenantA)
	grantFor(t, s, tenantA)
	assert.False(t, requestFor(s, tenantA).Grant, "A is capped at PerTenantMax")

	grantFor(t, s, tenantB)
	grantFor(t, s, tenantB)
	assert.Equal(t, 4, s.ActiveCount(), "the global cap is shared across tenants, not monopolized")

	assert.False(t, requestFor(s, uuid.New()).Grant, "global cap now full")
}

func TestScheduler_AgingShortensRetryForLongWaiters(t *testing.T) {
	s, _, cur := fullHeadroomScheduler()
	fillGlobalCap(t, s)

	waiter := uuid.New()
	first := s.RequestSlot(waiter, uuid.New(), SlotRequest{})
	require.False(t, first.Grant)

	*cur = cur.Add(15 * time.Second)
	later := s.RequestSlot(waiter, uuid.New(), SlotRequest{})
	require.False(t, later.Grant)
	assert.Less(t, later.RetryAfter, first.RetryAfter, "a longer wait earns a shorter retry")
}

func TestScheduler_ExpiredGrantFreesASlot(t *testing.T) {
	s, _, cur := fullHeadroomScheduler()
	fillGlobalCap(t, s)
	assert.False(t, requestFor(s, uuid.New()).Grant)

	*cur = cur.Add(schedCfg().GrantTTL + time.Second)
	assert.True(t, requestFor(s, uuid.New()).Grant)
}

func TestScheduler_RenewIsIdempotentWithinTTL(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	agent, tenant := uuid.New(), uuid.New()

	for range 2 {
		require.True(t, s.RequestSlot(agent, tenant, SlotRequest{}).Grant)
	}
	assert.Equal(t, 1, s.ActiveCount(), "renew must not double-book a slot")
}

func TestScheduler_ReleaseFreesTheSlot(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	agent := uuid.New()
	require.True(t, s.RequestSlot(agent, uuid.New(), SlotRequest{}).Grant)
	assert.Equal(t, 1, s.ActiveCount())
	s.Release(agent)
	assert.Equal(t, 0, s.ActiveCount())
	s.Release(uuid.New())
	assert.Equal(t, 0, s.ActiveCount())
	var nilSched *BackfillScheduler
	assert.NotPanics(t, func() { nilSched.Release(uuid.New()) })
}

func TestScheduler_ReleaseDecrementsSharedTenantCount(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()
	a1, a2 := uuid.New(), uuid.New()
	require.True(t, s.RequestSlot(a1, tenant, SlotRequest{}).Grant)
	require.True(t, s.RequestSlot(a2, tenant, SlotRequest{}).Grant)

	s.Release(a1)
	assert.Equal(t, 1, s.ActiveCount())
	grantFor(t, s, tenant)
	assert.Equal(t, 2, s.ActiveCount())
}

func TestDefaultBackfillSchedulerConfigPinsDurations(t *testing.T) {
	cfg := DefaultBackfillSchedulerConfig()
	assert.Equal(t, 60*time.Second, cfg.GrantTTL)
	assert.Equal(t, 30*time.Second, cfg.DeferBackoff)
}

func TestScheduler_ReleaseLastGrantRemovesTenantCounter(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()
	agent := uuid.New()
	require.True(t, s.RequestSlot(agent, tenant, SlotRequest{}).Grant)
	require.Equal(t, 1, s.tenantCount[tenant])

	s.Release(agent)

	_, exists := s.tenantCount[tenant]
	assert.False(t, exists)
}
