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

// requestFor asks for a grant for a fresh agent of the tenant.
func requestFor(s *BackfillScheduler, tenant uuid.UUID) BackfillDecision {
	return s.RequestSlot(uuid.New(), tenant, SlotRequest{})
}

// grantedAgent returns a fresh agent of the tenant holding a live grant.
func grantedAgent(t *testing.T, s *BackfillScheduler, tenant uuid.UUID) uuid.UUID {
	t.Helper()
	agent := uuid.New()
	s.RequestSlot(agent, tenant, SlotRequest{})
	return agent
}

// admitted requires a granted agent of the tenant to take a place, and returns how to give it back.
func admitted(t *testing.T, s *BackfillScheduler, tenant uuid.UUID) func() {
	t.Helper()
	agent := grantedAgent(t, s, tenant)
	release, admission := s.Admit(agent, tenant)
	require.True(t, admission.Admitted)
	return release
}

// fillGlobalPlaces takes every place, one for each of four distinct tenants.
func fillGlobalPlaces(t *testing.T, s *BackfillScheduler) {
	t.Helper()
	for range 4 {
		admitted(t, s, uuid.New())
	}
}

func TestScheduler_EveryRequestIsGrantedAndAGrantHoldsNoPlace(t *testing.T) {
	s, clock, _ := fullHeadroomScheduler()

	for i := range schedCfg().MaxConcurrent + 1 {
		d := s.RequestSlot(uuid.New(), uuid.New(), SlotRequest{PendingSamples: 100})
		assert.Positive(t, d.Rate, "request %d is granted past the place count", i)
		assert.GreaterOrEqual(t, d.Rate, schedCfg().MinGrantRate)
		assert.LessOrEqual(t, d.Rate, schedCfg().MaxGrantRate)
		assert.Equal(t, clock().Add(schedCfg().GrantTTL).Unix(), d.Deadline)
	}
	assert.Equal(t, 0, s.ActiveCount(), "a grant takes no place")
}

func TestScheduler_ABatchWithoutAGrantIsDeferred(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()

	release, admission := s.Admit(uuid.New(), uuid.New())
	assert.False(t, admission.Admitted)
	assert.Equal(t, BackfillNoGrant, admission.Reason)
	assert.Positive(t, admission.RetryAfter)
	assert.Nil(t, release)
	assert.Equal(t, 0, s.ActiveCount())
}

func TestScheduler_ABatchPastTheGlobalPlacesIsDeferredAsBusy(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	fillGlobalPlaces(t, s)

	tenant := uuid.New()
	release, admission := s.Admit(grantedAgent(t, s, tenant), tenant)
	assert.False(t, admission.Admitted)
	assert.Equal(t, BackfillBusy, admission.Reason)
	assert.Positive(t, admission.RetryAfter)
	assert.Nil(t, release)
	assert.Equal(t, 4, s.ActiveCount())
}

func TestScheduler_PerTenantPlacesDeferTheThirdBatchOfOneTenant(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()

	admitted(t, s, tenant)
	admitted(t, s, tenant)
	_, admission := s.Admit(grantedAgent(t, s, tenant), tenant)
	assert.False(t, admission.Admitted, "per-tenant places full")
	assert.Equal(t, BackfillBusy, admission.Reason)
}

func TestScheduler_BudgetShrinksUnderLiveLoad(t *testing.T) {
	s, clock, _ := fullHeadroomScheduler()

	full := requestFor(s, uuid.New())

	s2 := NewBackfillScheduler(schedCfg(), clock, func() float64 { return 0.25 })
	low := requestFor(s2, uuid.New())
	assert.Less(t, low.Rate, full.Rate, "lower headroom yields a lower granted rate")
}

func TestScheduler_FairShareCapsAnyOneTenant(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenantA, tenantB := uuid.New(), uuid.New()

	admitted(t, s, tenantA)
	admitted(t, s, tenantA)
	_, refused := s.Admit(grantedAgent(t, s, tenantA), tenantA)
	assert.False(t, refused.Admitted, "A is capped at PerTenantMax")

	admitted(t, s, tenantB)
	admitted(t, s, tenantB)
	assert.Equal(t, 4, s.ActiveCount(), "the global places are shared across tenants, not monopolized")

	other := uuid.New()
	_, full := s.Admit(grantedAgent(t, s, other), other)
	assert.False(t, full.Admitted, "global places now full")
}

func TestScheduler_AgingShortensRetryForLongWaiters(t *testing.T) {
	s, _, cur := fullHeadroomScheduler()
	fillGlobalPlaces(t, s)

	tenant := uuid.New()
	waiter := grantedAgent(t, s, tenant)
	_, first := s.Admit(waiter, tenant)
	require.False(t, first.Admitted)

	*cur = cur.Add(15 * time.Second)
	s.RequestSlot(waiter, tenant, SlotRequest{})
	_, later := s.Admit(waiter, tenant)
	require.False(t, later.Admitted)
	assert.Less(t, later.RetryAfter, first.RetryAfter, "a longer wait earns a shorter retry")
}

func TestScheduler_AnExpiredGrantAdmitsNothing(t *testing.T) {
	s, _, cur := fullHeadroomScheduler()
	tenant := uuid.New()
	agent := grantedAgent(t, s, tenant)

	*cur = cur.Add(schedCfg().GrantTTL + time.Second)
	_, admission := s.Admit(agent, tenant)
	assert.False(t, admission.Admitted)
	assert.Equal(t, BackfillNoGrant, admission.Reason)
}

func TestScheduler_ARenewedGrantOutlivesItsFirstDeadline(t *testing.T) {
	s, _, cur := fullHeadroomScheduler()
	agent, tenant := uuid.New(), uuid.New()
	s.RequestSlot(agent, tenant, SlotRequest{})

	*cur = cur.Add(schedCfg().GrantTTL / 2)
	s.RequestSlot(agent, tenant, SlotRequest{})
	*cur = cur.Add(schedCfg().GrantTTL/2 + time.Second)

	release, admission := s.Admit(agent, tenant)
	require.True(t, admission.Admitted, "the renewal moved the deadline")
	release()
}

func TestScheduler_GivingBackAPlaceFreesItOnce(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()
	release := admitted(t, s, tenant)
	other := admitted(t, s, tenant)
	assert.Equal(t, 2, s.ActiveCount())

	release()
	release()
	assert.Equal(t, 1, s.ActiveCount(), "a second give-back frees nothing more")
	admitted(t, s, tenant)
	assert.Equal(t, 2, s.ActiveCount())
	other()
}

func TestScheduler_TheLastPlaceGivenBackRemovesTheTenantCounter(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()
	release := admitted(t, s, tenant)
	require.Equal(t, 1, s.tenantPlaces[tenant])

	release()

	_, exists := s.tenantPlaces[tenant]
	assert.False(t, exists)
}

func TestScheduler_ReleaseDropsTheGrant(t *testing.T) {
	s, _, _ := fullHeadroomScheduler()
	tenant := uuid.New()
	agent := grantedAgent(t, s, tenant)

	s.Release(agent)
	_, admission := s.Admit(agent, tenant)
	assert.Equal(t, BackfillNoGrant, admission.Reason)
	s.Release(uuid.New())
	var nilSched *BackfillScheduler
	assert.NotPanics(t, func() { nilSched.Release(uuid.New()) })
}

func TestScheduler_ANilSchedulerAdmitsNothing(t *testing.T) {
	var nilSched *BackfillScheduler
	release, admission := nilSched.Admit(uuid.New(), uuid.New())
	assert.False(t, admission.Admitted)
	assert.Equal(t, BackfillNoGrant, admission.Reason)
	assert.Nil(t, release)
	assert.Zero(t, nilSched.ActiveCount(), "a nil scheduler holds no place")
}

func TestDefaultBackfillSchedulerConfigPinsDurations(t *testing.T) {
	cfg := DefaultBackfillSchedulerConfig()
	assert.Equal(t, 60*time.Second, cfg.GrantTTL)
	assert.Equal(t, 30*time.Second, cfg.DeferBackoff)
}
