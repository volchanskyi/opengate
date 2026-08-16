// Package alerts owns what a machine reported was wrong, the room those reports
// fold into, and the erasure of both.
//
// An alert is the only carrier of the detail behind a signal: central keeps one
// 60 s average per dimension and there is no path for asking the endpoint
// afterwards, so whatever arrives on the alert is the whole of what will ever be
// known about that moment. That shapes everything here — evidence rides on the
// alert row rather than anywhere it could be fetched from, an alert is written
// whole or not at all, and a customer's hourly ceiling counts what it refuses
// instead of dropping it quietly.
//
// Isolation is the tenant's, enforced by row-level security in Postgres. Every
// scoping key is the customer's, because the customer is who the work is for:
// one customer's storm must not spend another's budget, and two customers'
// unrelated outages must not land in one room.
package alerts

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxEvidenceBytes is the most an alert's compressed evidence may weigh. It
	// matches the wire contract's cap and the database's own check, which is the
	// one that finally refuses: evidence is immutable and unfetchable, so a blob
	// that slipped past an application check would sit in the table forever.
	MaxEvidenceBytes = 64 * 1024

	// OrganizationHourlyCeiling is how many alerts one customer may store per
	// rolling hour. It is roughly twelve times a customer's steady rate, so it
	// catches a storm without clipping ordinary operation.
	//
	// Per customer, never per tenant: at the tenant, one customer's bad night
	// would consume the budget of every other customer the MSP looks after, and
	// silencing detection across an estate is a worse failure than the storm.
	OrganizationHourlyCeiling = 500

	// StormRuleID names the room a customer's suppressed alerts fold into. It is
	// not a catalogue rule — nothing evaluates it on a machine — but an incident
	// has to be keyed on something, and keying it here means one storm is one
	// room however long it lasts.
	StormRuleID = "alert-storm"

	// StormSeverity is how bad a spent budget is. Worth a person's attention,
	// since detection past the ceiling is being refused, but not the same as a
	// machine that is breaking.
	StormSeverity = SeverityWarning
)

// Severity is how bad an alert or an incident is. The set is closed and the
// database enforces it: a severity nothing downstream can render would be stored
// happily and discovered by whoever opens the incident.
type Severity string

const (
	// SeverityInfo is worth recording beside an incident, not worth raising one.
	SeverityInfo Severity = "info"
	// SeverityWarning means something is wrong and a person should look.
	SeverityWarning Severity = "warning"
	// SeverityCritical means something is broken now.
	SeverityCritical Severity = "critical"
)

// Scope is how wide an incident is — the rung of the tenancy ladder its
// grouping key names. The customer is the broadest there is, because folding
// across customers would put two estates' unrelated events in one room with no
// correct assignee.
type Scope string

const (
	// ScopeDevice groups an incident on one machine.
	ScopeDevice Scope = "device"
	// ScopeSite groups an incident on one location or department.
	ScopeSite Scope = "site"
	// ScopeOrganization groups an incident across one customer's whole estate.
	ScopeOrganization Scope = "organization"
)

// Status is where an incident stands. An incident in StatusNew is the triage
// queue, which is why there is no separate promotion entity.
type Status string

const (
	// StatusNew is an incident nobody has picked up.
	StatusNew Status = "new"
	// StatusAcknowledged is one somebody has taken.
	StatusAcknowledged Status = "acknowledged"
	// StatusInvestigating is one being worked.
	StatusInvestigating Status = "investigating"
	// StatusResolved is one that is over.
	StatusResolved Status = "resolved"
)

// Outcome is what became of an alert offered to the store. It is deliberately
// three answers rather than an error and a success: a replay and a spent budget
// are both ordinary, and telling them apart is what lets each be counted under
// its own reason.
type Outcome string

const (
	// Stored means the alert became a row.
	Stored Outcome = "stored"
	// Duplicate means its identity was already stored, so a reconnect replaying
	// a queued alert changed nothing.
	Duplicate Outcome = "duplicate"
	// CeilingSuppressed means the customer's hourly budget was spent. The alert
	// is not stored; the storm incident carries the count of what was refused.
	CeilingSuppressed Outcome = "organization_ceiling"
)

// Alert is one thing a machine reported, with everything it knew about why.
type Alert struct {
	// ID is the id the device chose. It is not the alert's identity — an agent
	// that lost its local store picks a new one — so it never decides whether a
	// replay is a duplicate.
	ID uuid.UUID
	// OrganizationID is the customer the machine belongs to.
	OrganizationID uuid.UUID
	// DeviceID is the machine that raised it.
	DeviceID uuid.UUID
	// RuleID and RuleVersion name the rule that fired, as it stood then.
	RuleID      string
	RuleVersion uint32
	// Severity is one of the three.
	Severity Severity
	// Metric is the dimension the rule watched, empty for a rule that fires on
	// an event rather than a reading.
	Metric string
	// Value is the reading that crossed the threshold, absent for the same
	// reason Metric can be empty.
	Value *float64
	// WindowStart and WindowEnd are the interval the rule evaluated over.
	// WindowStart is part of the alert's identity.
	WindowStart time.Time
	WindowEnd   time.Time
	// ObservedAt is when the machine saw it.
	ObservedAt time.Time
	// Backfilled marks a finding a retroactive scan produced over local history
	// rather than a live firing. It still folds by its real event time.
	Backfilled bool
	// Evidence is everything the device knew about why this fired, compressed,
	// and EvidenceCodec names how. Both empty is a legal alert: a machine that
	// had nothing to attach still says it is in trouble.
	Evidence      []byte
	EvidenceCodec string
}

// Incident is the room a customer's alerts are investigated in.
type Incident struct {
	// ID identifies the room.
	ID uuid.UUID
	// OrganizationID is the customer it belongs to.
	OrganizationID uuid.UUID
	// RuleID keys the room on the rule, never the rule version: upgrading a rule
	// while an incident is open must not fork the room somebody is working in.
	RuleID string
	// Scope and ScopeKey are how wide the room is and what it is about.
	Scope    Scope
	ScopeKey uuid.UUID
	// Severity is the worst of what has folded in.
	Severity Severity
	// Status is where it stands.
	Status Status
	// AssigneeID is who is working it, zero when nobody has taken it. It is a
	// column as well as a line in the room's history because the queue is
	// filtered on it — "what am I holding" is the first question of a shift.
	AssigneeID uuid.UUID
	// OpenedAt is when the room was raised, which is receipt time rather than
	// event time: it says when the estate started being able to act.
	OpenedAt time.Time
	// ResolvedAt is when it ended, zero while it is open.
	ResolvedAt time.Time
	// CauseCode is the answer a person gave for closing it, empty while it is
	// open and when the system closed it — inventing one there would put a
	// technician's vocabulary in the system's mouth.
	CauseCode CauseCode
	// FirstSeen and LastSeen are event times, not receipt times: a retroactive
	// finding belongs where it happened, or a week-old freeze would sort as
	// today's.
	FirstSeen time.Time
	LastSeen  time.Time
	// Occurrences is how much has folded in, and DeviceCount across how many
	// machines. Both are application state — no foreign key can keep them true
	// when a machine is erased.
	Occurrences int
	DeviceCount int
}

// normalized returns the alert as it is stored: timestamps at the resolution
// Postgres keeps, and evidence either present with a codec naming it or absent
// with neither. An empty-but-not-nil blob would be stored as evidence that
// exists and cannot be read.
func (a Alert) normalized() Alert {
	a.WindowStart = a.WindowStart.UTC().Truncate(time.Microsecond)
	a.WindowEnd = a.WindowEnd.UTC().Truncate(time.Microsecond)
	a.ObservedAt = a.ObservedAt.UTC().Truncate(time.Microsecond)
	if len(a.Evidence) == 0 {
		a.Evidence = nil
		a.EvidenceCodec = ""
	}
	return a
}
