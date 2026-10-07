package alerts

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// StormHold is how long a customer's storm room stays open with nothing further refused.
// The storm room is no catalogue rule, so the hold is one rolling hour of the ceiling's budget.
const StormHold = time.Hour

// Errors a caller tells apart because each has a different fix.
var (
	// ErrGroupingUnusable means the rule's grouping cannot be acted on, which is a caller bug.
	ErrGroupingUnusable = errors.New("rule grouping is unusable")
	// ErrIncidentNotFound covers a room that does not exist and one in another tenant.
	ErrIncidentNotFound = errors.New("incident not found")
	// ErrUnknownStatus is a status outside the closed set.
	ErrUnknownStatus = errors.New("unknown incident status")
	// ErrUnknownCause is a cause code outside the closed set.
	ErrUnknownCause = errors.New("unknown resolution cause code")
	// ErrIllegalTransition is a move the lifecycle disallows, including an unchanged status.
	ErrIllegalTransition = errors.New("illegal incident transition")
	// ErrCauseRequired is a resolution with no answer for why.
	ErrCauseRequired = errors.New("resolving an incident requires a cause code")
	// ErrCauseNotAllowed is a cause code on a move other than a resolution.
	ErrCauseNotAllowed = errors.New("only a resolution carries a cause code")
	// ErrKeyAlreadyOpen means a newer room holds the grouping key, so the closed one cannot reopen.
	ErrKeyAlreadyOpen = errors.New("another open incident already holds this grouping key")
	// ErrAlertNotFound covers an alert absent from the room it was asked through, in any tenant.
	ErrAlertNotFound = errors.New("alert not found in this incident")
	// ErrNoEvidence means the alert carries no evidence.
	ErrNoEvidence = errors.New("alert carries no evidence")
	// ErrCommentUnusable is a comment that is empty or longer than the bound.
	ErrCommentUnusable = errors.New("comment is empty or too long")
)

// CauseCode is a person's answer for why an incident ended; the database enforces the closed set.
type CauseCode string

const (
	// CauseResolvedSelf means it stopped on its own.
	CauseResolvedSelf CauseCode = "resolved_self"
	// CauseFixedByTech means somebody fixed it.
	CauseFixedByTech CauseCode = "fixed_by_tech"
	// CauseHardwareFault means the machine is at fault and needs parts.
	CauseHardwareFault CauseCode = "hardware_fault"
	// CauseExpectedLoad means the reading was real and the work was meant to happen.
	CauseExpectedLoad CauseCode = "expected_load"
	// CauseFalsePositive means the rule was wrong; it is the feedback channel for rule thresholds.
	CauseFalsePositive CauseCode = "false_positive"
	// CauseDuplicate means another room already covers it.
	CauseDuplicate CauseCode = "duplicate"
	// CauseWontFix means it is real, understood, and being lived with.
	CauseWontFix CauseCode = "wont_fix"
)

var knownCauses = []CauseCode{
	CauseResolvedSelf, CauseFixedByTech, CauseHardwareFault, CauseExpectedLoad,
	CauseFalsePositive, CauseDuplicate, CauseWontFix,
}

// nextStatuses lists what may follow each state; a resolved room has no successor.
// Undoing a resolution is [Store.Reopen], not a transition.
var nextStatuses = map[Status][]Status{
	StatusNew:           {StatusAcknowledged, StatusInvestigating, StatusResolved},
	StatusAcknowledged:  {StatusNew, StatusInvestigating, StatusResolved},
	StatusInvestigating: {StatusNew, StatusAcknowledged, StatusResolved},
	StatusResolved:      nil,
}

// Grouping is how one rule's alerts collapse into rooms by width and by window.
// The fold window doubles as the auto-resolve hold, so the two agree by construction.
type Grouping struct {
	// Scope is the rung of the tenancy ladder a room is about, never wider than one customer.
	Scope Scope
	// Window is how far apart two firings on one key can be and still be one room, and the idle hold.
	Window time.Duration
}

// check refuses a scope outside the closed set and a window of zero or less.
func (g Grouping) check() error {
	switch g.Scope {
	case ScopeDevice, ScopeSite, ScopeOrganization:
	default:
		return fmt.Errorf("%w: scope %q is not a rung alerts can be grouped on", ErrGroupingUnusable, g.Scope)
	}
	if g.Window <= 0 {
		return fmt.Errorf("%w: rule %s declares no grouping window", ErrGroupingUnusable, g.Scope)
	}
	return nil
}

// spans reports whether an alert at time at belongs to a room covering [firstSeen, lastSeen].
// The test is two-sided so a backfill replayed newest-first still folds into one room.
func (g Grouping) spans(firstSeen, lastSeen, at time.Time) bool {
	return !at.After(lastSeen.Add(g.Window)) && !at.Before(firstSeen.Add(-g.Window))
}

// lapsed reports whether an alert at time at arrives after everything the room could still gather.
// An alert predating the room does not lapse it.
func (g Grouping) lapsed(lastSeen, at time.Time) bool {
	return at.After(lastSeen.Add(g.Window))
}

// Change is one move a person makes on an incident.
type Change struct {
	// To is where the incident should stand afterwards.
	To Status
	// Cause is why it ended, required when To is [StatusResolved] and refused otherwise.
	Cause CauseCode
	// Actor is who did it; the zero value is the system, which marks an auto-resolution.
	Actor uuid.UUID
}

// check validates the move out of from, naming the specific mistake for each rejection.
func (c Change) check(from Status) error {
	if _, known := nextStatuses[c.To]; !known {
		return fmt.Errorf("%w: %q", ErrUnknownStatus, c.To)
	}
	if c.Cause != "" && !knownCause(c.Cause) {
		return fmt.Errorf("%w: %q", ErrUnknownCause, c.Cause)
	}
	if !allows(from, c.To) {
		return fmt.Errorf("%w: %s to %s", ErrIllegalTransition, from, c.To)
	}
	if c.To == StatusResolved && c.Cause == "" {
		return ErrCauseRequired
	}
	if c.To != StatusResolved && c.Cause != "" {
		return fmt.Errorf("%w: %s carries %q", ErrCauseNotAllowed, c.To, c.Cause)
	}
	return nil
}

func allows(from, to Status) bool {
	for _, next := range nextStatuses[from] {
		if next == to {
			return true
		}
	}
	return false
}

func knownCause(cause CauseCode) bool {
	for _, known := range knownCauses {
		if known == cause {
			return true
		}
	}
	return false
}

// eventKind names a transition in the room's history; a resolution is its own kind for reports.
func eventKind(to Status) string {
	if to == StatusResolved {
		return kindResolution
	}
	return kindStatusChange
}

const (
	kindStatusChange = "status_change"
	kindResolution   = "resolution"
)

// transitionBody is what a status line in a room's history says, recording both ends of the move.
type transitionBody struct {
	From Status `json:"from"`
	To   Status `json:"to"`
	// Cause is present on a resolution only.
	Cause CauseCode `json:"cause_code,omitempty"`
	// Reopened marks the move that withdraws a resolution.
	Reopened bool `json:"reopened,omitempty"`
}

func (b transitionBody) json() ([]byte, error) {
	encoded, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encode incident event body: %w", err)
	}
	return encoded, nil
}
