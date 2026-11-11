package agentapi

import (
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/telemetry"
)

// BackfillSchedulerConfig tunes the admission control.
type BackfillSchedulerConfig struct {
	// BaseBudgetSamplesPerSec is the total catch-up pace at full headroom, shared by every grant.
	BaseBudgetSamplesPerSec int
	// MinGrantRate and MaxGrantRate bound one grant's samples a second; the floor never rises
	// above the machine's share of the budget.
	MinGrantRate uint32
	MaxGrantRate uint32
	// GrantTTL is how long a grant stays valid unless the machine asks again.
	GrantTTL time.Duration
	// Bound is the writer's catch-up queue in encoded bytes.
	Bound int
	// HighMark and LowMark are the queue fills that start and end throttling.
	HighMark float64
	LowMark  float64
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

// CatchUpBatch is one batch asking to be written.
type CatchUpBatch struct {
	Device   uuid.UUID
	Customer uuid.UUID
	// Key names the batch, so a second copy sent while the first is queued joins it.
	Key  string
	Size int
	// Answer tells the machine what became of the batch, on the connection that sent the copy.
	Answer func(telemetry.Outcome)
}

// Verdict is what admission did with a batch.
type Verdict uint8

const (
	// Refused is a batch to be sent again after RetryAfter.
	Refused Verdict = iota
	// Admitted is a batch to hand to the writer.
	Admitted
	// Joined is a copy of a batch already queued, which the queued batch's answer covers.
	Joined
)

// BackfillAdmission is the scheduler's answer to one catch-up batch.
type BackfillAdmission struct {
	Verdict    Verdict
	Reason     string
	RetryAfter uint32
}

// The reasons a catch-up batch is refused before its write.
const (
	// BackfillNoGrant is a batch from a machine holding no live grant.
	BackfillNoGrant = "no_grant"
	// BackfillBusy is a batch that does not fit the queue, its customer's share, or its turn.
	BackfillBusy = "busy"
)

const (
	// minRetryAfter and maxRetryAfter bound the wait a refused machine is given.
	minRetryAfter = time.Second
	maxRetryAfter = 30 * time.Second
	// encodedReadingBytes is about one catch-up reading in the store's import format.
	encodedReadingBytes = 160
)

// DefaultBackfillSchedulerConfig returns the single-node starting values over the writer's own
// catch-up queue, with Zabbix's 80% and 20% throttling marks.
func DefaultBackfillSchedulerConfig() BackfillSchedulerConfig {
	return BackfillSchedulerConfig{
		BaseBudgetSamplesPerSec: 20_000,
		MinGrantRate:            500,
		MaxGrantRate:            5_000,
		GrantTTL:                60 * time.Second,
		Bound:                   telemetry.DefaultWriterConfig().CatchUpBound,
		HighMark:                0.8,
		LowMark:                 0.2,
	}
}
