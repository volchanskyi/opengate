package agentapi

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// measureDrain folds written bytes into the measured drain rate once a second's worth passed.
func (s *BackfillScheduler) measureDrain(written int) {
	s.drained += written
	elapsed := s.now().Sub(s.drainedSince).Seconds()
	if elapsed < 1 {
		return
	}
	s.drainRate = 0.5*s.drainRate + 0.5*float64(s.drained)/elapsed
	s.drained = 0
	s.drainedSince = s.now()
}

// refuse tells a machine to wait for the queue's estimated drain time, spread by its place among
// its customer's waiting machines when throttled.
func (s *BackfillScheduler) refuse(turns *customerTurns, machine uuid.UUID, reason string) BackfillAdmission {
	wait := float64(s.held) / max(s.drainRate, 1)
	if turns != nil && s.throttled {
		if pos, waiting := turns.position(machine); waiting > 0 {
			wait = wait * float64(pos+1) / float64(waiting)
		}
	}
	retry := min(max(time.Duration(wait*float64(time.Second)), minRetryAfter), maxRetryAfter)
	return BackfillAdmission{Reason: reason, RetryAfter: uint32(math.Ceil(retry.Seconds()))}
}

// rate is the machine's share of the load-adaptive budget among every grant, up to the grant
// ceiling, with a floor never above that share.
func (s *BackfillScheduler) rate() uint32 {
	h := min(1, max(0, s.headroom()))
	share := max(1, uint32(float64(s.cfg.BaseBudgetSamplesPerSec)*h/float64(max(1, len(s.grants)))))
	floor := min(s.cfg.MinGrantRate, share)
	return min(max(share, floor), s.cfg.MaxGrantRate)
}
