package alerts

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// How alerts collapse into rooms, and how a room is allowed to move.
//
// Grouping has two axes and both are load-bearing. How wide a room is turns 312
// alerts from a driver rollout into one thing a person reads; how long firings
// stay one turns thirty daily freezes on one workstation into a pattern instead
// of thirty one-offs. Neither axis alone does the work: the same thirty freezes
// under a half-hour window are thirty rooms and the diagnosis is gone.

// StormHold is how long a customer's storm room stays open with nothing further
// refused. No catalogue rule can supply it — the storm room is not a rule — and
// an hour is the right figure because a storm is precisely a rolling hour's
// budget being spent: an hour with nothing refused is a storm that is over.
const StormHold = time.Hour

// Errors a caller has to be able to tell apart, because each has a different
// fix: a wiring bug, a form filled in wrong, a room somebody else already
// closed, a key that has moved on.
var (
	// ErrGroupingUnusable means the rule's grouping cannot be acted on. It is a
	// caller bug rather than a bad alert, so it is reported instead of being
	// papered over with a default — a guessed window folds unrelated firings
	// together and nothing downstream would ever say so.
	ErrGroupingUnusable = errors.New("rule grouping is unusable")
	// ErrIncidentNotFound covers a room that does not exist and one in another
	// tenant, which must be indistinguishable.
	ErrIncidentNotFound = errors.New("incident not found")
	// ErrUnknownStatus is a status outside the closed set.
	ErrUnknownStatus = errors.New("unknown incident status")
	// ErrUnknownCause is a cause code outside the closed set.
	ErrUnknownCause = errors.New("unknown resolution cause code")
	// ErrIllegalTransition is a move the lifecycle does not allow, including
	// standing still: an unchanged status is not a transition, and recording one
	// would put a line in a handover timeline that says nothing happened.
	ErrIllegalTransition = errors.New("illegal incident transition")
	// ErrCauseRequired is a resolution with no answer for why.
	ErrCauseRequired = errors.New("resolving an incident requires a cause code")
	// ErrCauseNotAllowed is a cause code on a move that is not a resolution.
	ErrCauseNotAllowed = errors.New("only a resolution carries a cause code")
	// ErrKeyAlreadyOpen means a newer room now holds the grouping key, so the
	// closed one cannot come back: there is exactly one open room per key, and
	// the live one is where the alerts are landing.
	ErrKeyAlreadyOpen = errors.New("another open incident already holds this grouping key")
)

// CauseCode is a person's answer for why an incident ended. The set is closed
// and the database enforces it: a code nothing can report on would be stored
// happily and discovered by whoever tries to count them.
type CauseCode string

const (
	// CauseResolvedSelf means it stopped on its own.
	CauseResolvedSelf CauseCode = "resolved_self"
	// CauseFixedByTech means somebody fixed it.
	CauseFixedByTech CauseCode = "fixed_by_tech"
	// CauseHardwareFault means the machine is at fault and needs parts.
	CauseHardwareFault CauseCode = "hardware_fault"
	// CauseExpectedLoad means the reading was real and the work was meant to
	// happen.
	CauseExpectedLoad CauseCode = "expected_load"
	// CauseFalsePositive means the rule was wrong. It is the load-bearing one:
	// it is the only channel that says which curated rule needs its threshold
	// moved, so a resolution that skips it spends feedback the pack is tuned
	// from.
	CauseFalsePositive CauseCode = "false_positive"
	// CauseDuplicate means another room already covers it.
	CauseDuplicate CauseCode = "duplicate"
	// CauseWontFix means it is real, understood, and being lived with.
	CauseWontFix CauseCode = "wont_fix"
)

// knownCauses is the closed set, in the order an operator would meet them.
var knownCauses = []CauseCode{
	CauseResolvedSelf, CauseFixedByTech, CauseHardwareFault, CauseExpectedLoad,
	CauseFalsePositive, CauseDuplicate, CauseWontFix,
}

// nextStatuses is the lifecycle, written as what may follow each state.
//
// Forward is the ordinary path and every skip along it is allowed: plenty of
// incidents are picked up and closed in one move. Backward stops at the queue —
// a technician going off shift hands a room back rather than leaving it looking
// worked — and a closed room has no successor at all, because undoing an answer
// that has been given is [Store.Reopen] rather than a transition.
var nextStatuses = map[Status][]Status{
	StatusNew:           {StatusAcknowledged, StatusInvestigating, StatusResolved},
	StatusAcknowledged:  {StatusNew, StatusInvestigating, StatusResolved},
	StatusInvestigating: {StatusNew, StatusAcknowledged, StatusResolved},
	StatusResolved:      nil,
}

// Grouping is how one rule's alerts collapse into rooms: how wide a room is, and
// how long firings on one key stay one room.
//
// There is deliberately no separate reopen window. A room must stay open for
// exactly as long as a new alert could still fold into it, and the fold's own
// window is that duration by definition. Anything longer leaves a room open that
// an arriving alert cannot join, and the one-open-room-per-key rule then has
// nowhere to put it; anything shorter closes a room while its next occurrence is
// still due, which is how a recurrence becomes a queue of one-offs. Either way
// auto-resolve and grouping would disagree, and this is the one number that
// makes them agree by construction.
type Grouping struct {
	// Scope is the rung of the tenancy ladder a room is about. The customer is
	// the widest there is: folding across customers would put two estates'
	// unrelated events in one room with no correct assignee.
	Scope Scope
	// Window is how far apart two firings on one key can be and still be the
	// same thing. It is also the hold before an idle room resolves itself.
	Window time.Duration
}

// check refuses a grouping that cannot be acted on. Both refusals are wiring
// bugs with quiet consequences: a scope outside the closed set cannot be stored
// at all, and a window of zero folds every firing a rule ever produces into one
// room that never resolves.
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

// spans reports whether an alert at time at belongs to a room already covering
// [firstSeen, lastSeen].
//
// The comparison is against the room's own span rather than the wall clock, and
// that is what makes a retroactive scan one room. Thirty findings from a month
// of local history all arrive in the same second; judged against now, twenty-nine
// of them would look a month stale and open a room each. It is two-sided for the
// same reason: a scan walking history backwards produces its findings
// newest-first, so a fold written only to extend forwards would fragment it.
func (g Grouping) spans(firstSeen, lastSeen, at time.Time) bool {
	return !at.After(lastSeen.Add(g.Window)) && !at.Before(firstSeen.Add(-g.Window))
}

// lapsed reports whether an alert at time at arrives after everything the room
// could still gather. Distinct from simply "outside the span": a finding that
// predates a room says nothing about whether that room is still live, and
// closing a room on the strength of a three-month-old retroactive finding would
// end work somebody is in the middle of.
func (g Grouping) lapsed(lastSeen, at time.Time) bool {
	return at.After(lastSeen.Add(g.Window))
}

// Change is one move a person makes on an incident.
type Change struct {
	// To is where the incident should stand afterwards.
	To Status
	// Cause is why it ended, required when To is [StatusResolved] and refused
	// otherwise.
	Cause CauseCode
	// Actor is who did it. The zero value is the system, which is how an
	// auto-resolution is told apart from somebody's decision.
	Actor uuid.UUID
}

// check validates the move out of from, naming what is wrong rather than
// refusing as one undifferentiated rejection: each of these is a different
// mistake with a different fix, and an API above has to answer differently for
// each.
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

// allows reports whether the lifecycle permits from -> to.
func allows(from, to Status) bool {
	for _, next := range nextStatuses[from] {
		if next == to {
			return true
		}
	}
	return false
}

// knownCause reports whether a cause code is one the closed set carries.
func knownCause(cause CauseCode) bool {
	for _, known := range knownCauses {
		if known == cause {
			return true
		}
	}
	return false
}

// eventKind names a transition in the room's history. A resolution is its own
// kind because it is the line a report counts, and because it is the one that
// carries the answer a rule is retuned from.
func eventKind(to Status) string {
	if to == StatusResolved {
		return kindResolution
	}
	return kindStatusChange
}

// Kinds of line a room's history carries. Only the two this engine writes are
// named here.
const (
	kindStatusChange = "status_change"
	kindResolution   = "resolution"
)

// transitionBody is what a status line in a room's history says. Both ends are
// recorded rather than just the destination: a handover reads the timeline, and
// "acknowledged" alone does not say whether somebody picked the room up or put
// it back down.
type transitionBody struct {
	From Status `json:"from"`
	To   Status `json:"to"`
	// Cause is present on a resolution and absent everywhere else.
	Cause CauseCode `json:"cause_code,omitempty"`
	// Reopened marks the one move that undoes an answer already given, so a
	// timeline shows a withdrawn resolution rather than an ordinary step.
	Reopened bool `json:"reopened,omitempty"`
}

// json renders the body for the event row.
func (b transitionBody) json() ([]byte, error) {
	encoded, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encode incident event body: %w", err)
	}
	return encoded, nil
}
