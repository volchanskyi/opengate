package agentapi

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

func TestACustomerMaySendOneBatchATurnWhateverItsSize(t *testing.T) {
	s, fill, _ := testScheduler()
	fill.Store(0.9)
	customers := make([]uuid.UUID, 4)
	for i := range customers {
		customers[i] = uuid.New()
	}
	share := schedCfg().Bound / len(customers)
	oversized := granted(s, customers[0])
	for _, c := range customers[1:] {
		granted(s, c)
	}

	require.Equal(t, Admitted, s.Admit(batch(oversized, customers[0], "k", 3*share, nil)).Verdict,
		"a batch larger than the customer's share still goes in its turn")
	s.Complete(oversized, telemetry.Outcome{Stored: 1})
	assert.Equal(t, Refused, s.Admit(batch(oversized, customers[0], "k2", share, nil)).Verdict,
		"and the overshoot is taken from its later turns")
}

// turnsTaken drives a throttled queue: each round every waiting machine that is not in flight
// asks, and every admitted batch is written by the end of the round.
func turnsTaken(t *testing.T, sizes map[uuid.UUID]int, machines map[uuid.UUID][]uuid.UUID, rounds int) map[uuid.UUID]int {
	t.Helper()
	s, fill, cur := testScheduler()
	fill.Store(0.9)
	order := grantAll(s, machines)
	turns := map[uuid.UUID]int{}
	for range rounds {
		for _, machine := range askOnce(s, order, machines, sizes, turns) {
			s.Complete(machine, telemetry.Outcome{Stored: 1})
		}
		*cur = cur.Add(time.Second)
		grantAll(s, machines)
	}
	return turns
}

// grantAll gives every machine a grant and returns its customers in the order the round asks them.
func grantAll(s *BackfillScheduler, machines map[uuid.UUID][]uuid.UUID) []uuid.UUID {
	order := make([]uuid.UUID, 0, len(machines))
	for customer, owned := range machines {
		order = append(order, customer)
		for _, machine := range owned {
			s.RequestSlot(machine, customer, SlotRequest{})
		}
	}
	return order
}

// askOnce has every machine ask once, customer by customer, and returns the machines admitted.
func askOnce(s *BackfillScheduler, order []uuid.UUID, machines map[uuid.UUID][]uuid.UUID,
	sizes, turns map[uuid.UUID]int) []uuid.UUID {
	var sent []uuid.UUID
	for _, customer := range order {
		for _, machine := range machines[customer] {
			if s.Admit(batch(machine, customer, fmt.Sprint(turns[customer]), sizes[customer], nil)).Verdict == Admitted {
				turns[customer]++
				sent = append(sent, machine)
			}
		}
	}
	return sent
}

func TestEveryWaitingCustomerGetsTurnsWhateverItsBatchSize(t *testing.T) {
	t.Parallel()
	for _, waiting := range []int{1, 3, 60, 500} {
		t.Run(fmt.Sprintf("%d customers", waiting), func(t *testing.T) {
			t.Parallel()
			sizes := map[uuid.UUID]int{}
			machines := map[uuid.UUID][]uuid.UUID{}
			for i := range waiting {
				customer := uuid.New()
				machines[customer] = []uuid.UUID{uuid.New()}
				sizes[customer] = batchBytes / 4
				if i%3 == 0 {
					sizes[customer] = 2 * batchBytes
				}
			}
			turns := turnsTaken(t, sizes, machines, 3*waiting)
			for customer := range machines {
				assert.Positivef(t, turns[customer], "a customer sending %d-byte batches got no turn", sizes[customer])
			}
		})
	}
}

func TestASmallCustomerProgressesBesideALargeOne(t *testing.T) {
	t.Parallel()
	dental, hospital := uuid.New(), uuid.New()
	machines := map[uuid.UUID][]uuid.UUID{dental: make([]uuid.UUID, 5), hospital: make([]uuid.UUID, 2000)}
	for _, owned := range machines {
		for i := range owned {
			owned[i] = uuid.New()
		}
	}
	sizes := map[uuid.UUID]int{dental: batchBytes, hospital: batchBytes}

	turns := turnsTaken(t, sizes, machines, 20)
	assert.InDelta(t, turns[hospital], turns[dental], 2,
		"the five-PC clinic gets as many turns as the 2,000-machine hospital")
	assert.GreaterOrEqual(t, turns[dental], 5, "every clinic machine has sent a batch")
}

func TestTheMachinesOfOneCustomerTakeTurns(t *testing.T) {
	s, fill, _ := testScheduler()
	fill.Store(0.9)
	customer := uuid.New()
	first, second := granted(s, customer), granted(s, customer)
	busy := uuid.New()
	busyMachine := granted(s, busy)
	require.Equal(t, Admitted, s.Admit(batch(busyMachine, busy, "k", batchBytes, nil)).Verdict)
	require.Equal(t, Admitted, s.Admit(batch(first, customer, "k", batchBytes, nil)).Verdict)

	assert.Equal(t, Refused, s.Admit(batch(second, customer, "k", batchBytes, nil)).Verdict)
	s.Complete(first, telemetry.Outcome{Stored: 1})
	s.Complete(busyMachine, telemetry.Outcome{Stored: 1})
	assert.Equal(t, Refused, s.Admit(batch(first, customer, "k2", batchBytes, nil)).Verdict,
		"the machine that just sent waits behind the one that asked before it")
	assert.Equal(t, Admitted, s.Admit(batch(second, customer, "k", batchBytes, nil)).Verdict)
}

func TestARefusedBatchWaitsForTheQueueToDrainSpreadByPosition(t *testing.T) {
	s, fill, _ := testScheduler()
	fill.Store(0.9)
	customer := uuid.New()
	holder := granted(s, customer)
	require.Equal(t, Admitted, s.Admit(batch(holder, customer, "k", batchBytes, nil)).Verdict)

	var retries []uint32
	for range 4 {
		admission := s.Admit(batch(granted(s, customer), customer, "k", batchBytes, nil))
		require.Equal(t, Refused, admission.Verdict)
		retries = append(retries, admission.RetryAfter)
	}
	for i, retry := range retries {
		assert.GreaterOrEqual(t, retry, uint32(1))
		assert.LessOrEqual(t, retry, uint32(30))
		if i > 0 {
			assert.GreaterOrEqual(t, retry, retries[i-1], "a machine further back waits at least as long")
		}
	}
}
