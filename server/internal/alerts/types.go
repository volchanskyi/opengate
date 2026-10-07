// Package alerts owns machine-reported alerts, the incident rooms they fold into, and the
// erasure of both under tenant row-level security.
package alerts

import (
	"time"

	"github.com/google/uuid"
)

const (
	// MaxEvidenceBytes is the most an alert's compressed evidence may weigh; it matches the wire
	// cap and the database check that finally refuses.
	MaxEvidenceBytes = 64 * 1024

	// StormRuleID keys the one room a customer's suppressed alerts fold into; no machine evaluates it.
	StormRuleID = "alert-storm"

	// StormSeverity is the severity of a spent budget: a warning, since detection past it is refused.
	StormSeverity = SeverityWarning
)

// Severity is how bad an alert or an incident is, from a closed set the database enforces.
type Severity string

const (
	// SeverityInfo is recorded beside an incident and raises none.
	SeverityInfo Severity = "info"
	// SeverityWarning means something is wrong and a person should look.
	SeverityWarning Severity = "warning"
	// SeverityCritical means something is broken now.
	SeverityCritical Severity = "critical"
)

// Scope is how wide an incident is: the tenancy rung its grouping key names, at most one customer.
type Scope string

const (
	// ScopeDevice groups an incident on one machine.
	ScopeDevice Scope = "device"
	// ScopeSite groups an incident on one location or department.
	ScopeSite Scope = "site"
	// ScopeOrganization groups an incident across one customer's whole estate.
	ScopeOrganization Scope = "organization"
)

// Status is where an incident stands; a StatusNew incident is the triage queue.
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

// Outcome is what became of an alert offered to the store; a replay and a spent budget are
// ordinary outcomes counted under their own reasons.
type Outcome string

const (
	// Stored means the alert became a row.
	Stored Outcome = "stored"
	// Duplicate means its identity was already stored, so a replayed queued alert changed nothing.
	Duplicate Outcome = "duplicate"
	// CeilingSuppressed means the customer's hourly budget was spent; the storm incident counts it.
	CeilingSuppressed Outcome = "organization_ceiling"
)

// Alert is one thing a machine reported, with everything it knew about why.
type Alert struct {
	// ID is the id the device chose; a replay is judged by the alert's identity, never by this id.
	ID             uuid.UUID
	OrganizationID uuid.UUID
	DeviceID       uuid.UUID
	RuleID         string
	RuleVersion    uint32
	Severity       Severity
	// Metric is the dimension the rule watched, empty for an event rule.
	Metric string
	// Value is the reading that crossed the threshold, absent for an event rule.
	Value *float64
	// WindowStart and WindowEnd bound the evaluation interval; WindowStart is part of the identity.
	WindowStart time.Time
	WindowEnd   time.Time
	ObservedAt  time.Time
	// Backfilled marks a finding from a retroactive scan of local history; it folds by event time.
	Backfilled bool
	// Evidence is compressed and EvidenceCodec names how; both empty is legal for a bare alert.
	Evidence      []byte
	EvidenceCodec string
}

// Incident is the room a customer's alerts are investigated in.
type Incident struct {
	ID             uuid.UUID
	OrganizationID uuid.UUID
	// RuleID keys the room on the rule, so upgrading a rule never forks an open room.
	RuleID   string
	Scope    Scope
	ScopeKey uuid.UUID
	// ScopeName names the host, site or customer ScopeKey points at, empty once that is removed.
	ScopeName string
	// Severity is the worst of what has folded in.
	Severity Severity
	Status   Status
	// AssigneeID is zero when nobody has taken it; it is a column because the queue filters on it.
	AssigneeID uuid.UUID
	// OpenedAt is receipt time, the moment the estate could start to act.
	OpenedAt time.Time
	// ResolvedAt is zero while the room is open.
	ResolvedAt time.Time
	// CauseCode is a person's answer for closing, empty while open and when the system closed it.
	CauseCode CauseCode
	// FirstSeen and LastSeen are event times, so a retroactive finding sorts where it happened.
	FirstSeen time.Time
	LastSeen  time.Time
	// Occurrences and DeviceCount are application state; no foreign key keeps them true on erasure.
	Occurrences int
	DeviceCount int
}

// normalized returns the alert as stored: microsecond UTC times, evidence with a codec or neither.
// An empty non-nil blob would be stored as evidence that exists and cannot be read.
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
