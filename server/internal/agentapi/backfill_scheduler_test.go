package agentapi

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// batchBytes is one full catch-up batch: a thousand readings at about 160 encoded bytes each.
const batchBytes = 160_000

// schedCfg is the shipped configuration over a queue of ten full batches.
func schedCfg() BackfillSchedulerConfig {
	cfg := DefaultBackfillSchedulerConfig()
	cfg.Bound = 10 * batchBytes
	return cfg
}

// fixedClock returns a controllable clock and a pointer to advance it.
func fixedClock() (func() time.Time, *time.Time) {
	base := time.Unix(1_700_000_000, 0).UTC()
	cur := &base
	return func() time.Time { return *cur }, cur
}

// testScheduler builds a scheduler on the fixed clock whose queue fill the test sets.
func testScheduler() (*BackfillScheduler, *atomic.Value, *time.Time) {
	clock, cur := fixedClock()
	var fill atomic.Value
	fill.Store(0.0)
	s := NewBackfillScheduler(schedCfg(), clock, nil, func() float64 { return fill.Load().(float64) })
	return s, &fill, cur
}

// granted returns a fresh machine of the customer holding a grant.
func granted(s *BackfillScheduler, customer uuid.UUID) uuid.UUID {
	machine := uuid.New()
	s.RequestSlot(machine, customer)
	return machine
}

// answerLog records each answer a batch's sender received.
type answerLog struct{ got []string }

func (l *answerLog) to(who string) func(telemetry.Outcome) {
	return func(o telemetry.Outcome) {
		if o.Err != nil {
			l.got = append(l.got, who+": "+o.Err.Error())
			return
		}
		l.got = append(l.got, who+": stored")
	}
}

func batch(machine, customer uuid.UUID, key string, size int, answer func(telemetry.Outcome)) CatchUpBatch {
	return CatchUpBatch{Device: machine, Customer: customer, Key: key, Size: size, Answer: answer}
}

// grantedMachine builds a scheduler holding a grant for one machine of a fresh customer.
func grantedMachine() (s *BackfillScheduler, cur *time.Time, customer, machine uuid.UUID) {
	s, _, cur = testScheduler()
	customer = uuid.New()
	return s, cur, customer, granted(s, customer)
}

// sendFull offers one full batch named key from the machine.
func sendFull(s *BackfillScheduler, machine, customer uuid.UUID, key string, answer func(telemetry.Outcome)) BackfillAdmission {
	return s.Admit(batch(machine, customer, key, batchBytes, answer))
}

func TestEveryRequestIsGrantedAndAGrantHoldsNoPlace(t *testing.T) {
	s, _, _ := testScheduler()
	for range 50 {
		decision := s.RequestSlot(uuid.New(), uuid.New())
		assert.NotZero(t, decision.Deadline)
	}
	assert.Zero(t, s.ActiveCount(), "a grant is permission, not a place in the queue")
}

func TestABatchWithoutAGrantIsToldToWait(t *testing.T) {
	s, _, _ := testScheduler()
	admission := s.Admit(batch(uuid.New(), uuid.New(), "rollup1m:60", batchBytes, nil))
	assert.Equal(t, Refused, admission.Verdict)
	assert.Equal(t, BackfillNoGrant, admission.Reason)
	assert.GreaterOrEqual(t, admission.RetryAfter, uint32(1))
}

func TestAnExpiredGrantAdmitsNothing(t *testing.T) {
	s, cur, customer, machine := grantedMachine()
	*cur = cur.Add(schedCfg().GrantTTL + time.Second)
	assert.Equal(t, BackfillNoGrant, sendFull(s, machine, customer, "k", nil).Reason)
}

func TestARenewedGrantOutlivesItsFirstDeadlineAndAnUnrenewedOneBesideItDoesNot(t *testing.T) {
	s, cur, customer, renewing := grantedMachine()
	lapsing := granted(s, customer)
	ttl := schedCfg().GrantTTL

	*cur = cur.Add(ttl / 2)
	s.RequestSlot(renewing, customer)
	*cur = cur.Add(ttl/2 + time.Second)

	assert.Equal(t, BackfillNoGrant, sendFull(s, lapsing, customer, "k", nil).Reason,
		"the grant nobody renewed has run out")
	assert.Equal(t, Admitted, sendFull(s, renewing, customer, "k", nil).Verdict,
		"the renewed grant holds past the deadline it first had")
	*cur = cur.Add(ttl)
	assert.Equal(t, BackfillNoGrant, sendFull(s, renewing, customer, "k2", nil).Reason,
		"the renewed grant runs out at its own new deadline")
}

func TestReleaseDropsTheGrant(t *testing.T) {
	s, _, customer, machine := grantedMachine()
	s.Release(machine)
	assert.Equal(t, BackfillNoGrant, sendFull(s, machine, customer, "k", nil).Reason)
}

func TestANilSchedulerAdmitsNothing(t *testing.T) {
	var s *BackfillScheduler
	assert.Equal(t, Refused, s.Admit(batch(uuid.New(), uuid.New(), "k", 1, nil)).Verdict)
	assert.Zero(t, s.ActiveCount())
	s.Release(uuid.New())
}

func TestAMachineWithABatchInFlightIsToldToWait(t *testing.T) {
	s, _, customer, machine := grantedMachine()
	require.Equal(t, Admitted, sendFull(s, machine, customer, "rollup1m:60", nil).Verdict)

	second := sendFull(s, machine, customer, "rollup1m:120", nil)
	assert.Equal(t, Refused, second.Verdict, "one batch in flight per machine")
	assert.Equal(t, BackfillBusy, second.Reason)
}

func TestTheSameBatchArrivingTwiceJoinsTheFirstAndIsAnsweredOnTheNewestConnection(t *testing.T) {
	s, _, customer, machine := grantedMachine()
	var log answerLog
	require.Equal(t, Admitted, sendFull(s, machine, customer, "rollup1m:60", log.to("old link")).Verdict)

	again := sendFull(s, machine, customer, "rollup1m:60", log.to("new link"))
	assert.Equal(t, Joined, again.Verdict, "the copy joins the queued batch and makes no second write")

	s.Complete(machine, telemetry.Outcome{Stored: 1000})
	assert.Equal(t, []string{"new link: stored"}, log.got, "the answer goes to the machine's current connection")
	assert.Zero(t, s.ActiveCount())
}

func TestTheQueueTurnsThrottledAtEightyPercentAndBackAtTwenty(t *testing.T) {
	s, fill, _ := testScheduler()
	customer := uuid.New()
	for _, step := range []struct {
		fill      float64
		throttled bool
	}{
		{0.5, false}, {0.79, false}, {0.8, true}, {0.5, true}, {0.21, true}, {0.19, false}, {0.5, false},
	} {
		fill.Store(step.fill)
		machine := granted(s, customer)
		admission := s.Admit(batch(machine, customer, "k", 1, nil))
		assert.Equalf(t, step.throttled, s.Throttled(), "at %.0f%% full", step.fill*100)
		if admission.Verdict == Admitted {
			s.Complete(machine, telemetry.Outcome{})
		}
	}
}

func TestInNormalModeACustomerIsHeldToItsShare(t *testing.T) {
	s, _, _ := testScheduler()
	hospital, clinic := uuid.New(), uuid.New()
	granted(s, clinic)

	admitted := 0
	for range 10 {
		if s.Admit(batch(granted(s, hospital), hospital, "k", batchBytes, nil)).Verdict == Admitted {
			admitted++
		}
	}
	assert.Equal(t, 5, admitted, "with two customers catching up, each holds half the queue")
	clinicMachine := granted(s, clinic)
	assert.Equal(t, Admitted, s.Admit(batch(clinicMachine, clinic, "k", batchBytes, nil)).Verdict,
		"the five-PC clinic is admitted beside the hospital")
}

func TestTheGrantPaceIsTheBudgetSharedAmongTheMachinesCatchingUp(t *testing.T) {
	s, _, _ := testScheduler()
	cfg := schedCfg()
	first := s.RequestSlot(uuid.New(), uuid.New())
	assert.Equal(t, cfg.MaxGrantRate, first.Rate, "one machine alone may go as fast as a grant allows")

	var last BackfillDecision
	for range 999 {
		last = s.RequestSlot(uuid.New(), uuid.New())
	}
	assert.Equal(t, uint32(cfg.BaseBudgetSamplesPerSec/1000), last.Rate,
		"a thousand machines share the budget with no floor above their share")
}

func TestCompletingABatchGivesBackItsPlaceOnce(t *testing.T) {
	s, _, customer, machine := grantedMachine()
	var log answerLog
	require.Equal(t, Admitted, sendFull(s, machine, customer, "k", log.to("link")).Verdict)
	assert.Equal(t, 1, s.ActiveCount())

	s.Complete(machine, telemetry.Outcome{Err: errors.New("store unavailable")})
	s.Complete(machine, telemetry.Outcome{Stored: 1})
	assert.Zero(t, s.ActiveCount())
	assert.Equal(t, []string{"link: store unavailable"}, log.got, "a batch is answered once")
}

func TestDefaultBackfillSchedulerConfigPinsItsStartingValues(t *testing.T) {
	cfg := DefaultBackfillSchedulerConfig()
	assert.Equal(t, 20_000, cfg.BaseBudgetSamplesPerSec)
	assert.Equal(t, 60*time.Second, cfg.GrantTTL)
	assert.Equal(t, telemetry.DefaultWriterConfig().CatchUpBound, cfg.Bound, "admission fills the writer's own queue")
	assert.InDelta(t, 0.8, cfg.HighMark, 0)
	assert.InDelta(t, 0.2, cfg.LowMark, 0)
}
