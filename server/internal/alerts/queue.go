package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

// The triage queue: one page of the rooms somebody is expected to work, newest
// activity first.
//
// Newest activity rather than newest room, because a week-old incident that
// fired again this morning is today's work and one that opened this morning and
// went quiet is not. That ordering is also what makes the page a keyset: an
// offset over a queue that is being written to skips rows silently, and the row
// it skips is an incident nobody ever sees. So a page says where it ended and
// the next one starts there.
//
// The customer-scoped read and the whole-tenant read are two statements rather
// than one with an optional predicate, because they are answered from two
// different indexes and an optional predicate leaves which one to a guess. Both
// are single literals for the reason stated in postgres.go: a query assembled
// from pieces is indistinguishable, to anything reading this file, from one
// assembled from input.

const (
	// defaultQueuePage is how many rooms a caller that states no page size gets.
	defaultQueuePage = 50
	// maxQueuePage is the most one read can return. A queue is read to be worked
	// through, so a caller asking for the whole table is answered with a page
	// and the cursor after it — the same answer a caller asking for a sensible
	// number gets.
	maxQueuePage = 200
)

// queueForCustomerSQL reads one customer's rooms. Every filter is expressed as
// a sentinel comparison rather than a NULL test, so the statement is one shape
// whatever the caller narrowed on and the ordering column pair is always a plain
// comparison the customer-leading index answers directly.
//
// The all-zero uuid is the "not narrowing on this" sentinel, written out in each
// statement rather than concatenated in from a shared constant — for the reason
// postgres.go gives: a query assembled from pieces reads, to anything looking at
// this file, like one assembled from input.
const queueForCustomerSQL = `
	SELECT i.id, i.organization_id, i.rule_id, i.scope, i.scope_key, i.severity, i.status,
	       i.assignee_id, i.opened_at, i.first_seen, i.last_seen, i.resolved_at, i.cause_code,
	       i.occurrences, i.device_count
	  FROM incidents i
	 WHERE i.tenant_id = current_setting('app.current_tenant')::uuid
	   AND i.organization_id = $9::uuid
	   AND (i.last_seen, i.id) < ($6::timestamptz, $7::uuid)
	   AND (cardinality($1::text[]) = 0 OR i.status = ANY($1::text[]))
	   AND (cardinality($2::text[]) = 0 OR i.severity = ANY($2::text[]))
	   AND ($3::text = '' OR i.rule_id = $3::text)
	   AND ($4::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR i.assignee_id = $4::uuid)
	   AND ($5::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR EXISTS (
	            SELECT 1 FROM alerts a
	             WHERE a.incident_id = i.id AND a.device_id = $5::uuid))
	 ORDER BY i.last_seen DESC, i.id DESC
	 LIMIT $8::integer`

// queueForTenantSQL is the same read across every customer at once, which is
// what a technician covering an estate of them asks for. It cannot be the
// statement above with the customer predicate dropped: that one is ordered by
// customer first, so reading it across customers would sort the whole table to
// answer a page of fifty.
const queueForTenantSQL = `
	SELECT i.id, i.organization_id, i.rule_id, i.scope, i.scope_key, i.severity, i.status,
	       i.assignee_id, i.opened_at, i.first_seen, i.last_seen, i.resolved_at, i.cause_code,
	       i.occurrences, i.device_count
	  FROM incidents i
	 WHERE i.tenant_id = current_setting('app.current_tenant')::uuid
	   AND (i.last_seen, i.id) < ($6::timestamptz, $7::uuid)
	   AND (cardinality($1::text[]) = 0 OR i.status = ANY($1::text[]))
	   AND (cardinality($2::text[]) = 0 OR i.severity = ANY($2::text[]))
	   AND ($3::text = '' OR i.rule_id = $3::text)
	   AND ($4::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR i.assignee_id = $4::uuid)
	   AND ($5::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR EXISTS (
	            SELECT 1 FROM alerts a
	             WHERE a.incident_id = i.id AND a.device_id = $5::uuid))
	 ORDER BY i.last_seen DESC, i.id DESC
	 LIMIT $8::integer`

// Cursor is where a page ended: the room's last activity and its id, which
// together order the queue uniquely. Both are needed — two rooms can be seen at
// the same instant, and a cursor on the timestamp alone would either repeat them
// or lose one.
type Cursor struct {
	LastSeen time.Time
	ID       uuid.UUID
}

// IsZero reports whether the cursor names no position, which is what a first
// page starts from and what the last page hands back.
func (c Cursor) IsZero() bool { return c.ID == uuid.Nil }

// beyondTheQueue is where a first page starts: later than any moment a room can
// be seen at. Stating it as a value rather than as an absent one keeps the read
// a single comparison the index answers, instead of a branch the planner has to
// guess at.
var beyondTheQueue = Cursor{
	LastSeen: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	ID:       uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"),
}

// Filter is what a technician is looking at. Every field left at its zero value
// narrows nothing, so the empty filter is the whole tenant's queue.
type Filter struct {
	// OrganizationID narrows to one customer. It is a filter rather than a
	// permission: every member of a tenant may see every customer in it, and the
	// picker decides which one is on screen.
	OrganizationID uuid.UUID
	// Statuses and Severities narrow to a set, because a triage queue is
	// ordinarily read as "everything that is not resolved".
	Statuses   []Status
	Severities []Severity
	// RuleID narrows to one rule, which is how a bad rollout is looked at.
	RuleID string
	// DeviceID narrows to the rooms holding an alert one machine raised. A room
	// is not keyed on a machine — a customer-wide event is one room across forty
	// of them — so this is the machine's own view of what it is caught up in.
	DeviceID uuid.UUID
	// AssigneeID narrows to one technician's work.
	AssigneeID uuid.UUID
	// After is where the previous page ended.
	After Cursor
	// Limit is how many rooms to return, bounded by maxQueuePage.
	Limit int
}

// normalized fills in the page bound and the starting position, so the statement
// below is one shape for every caller.
func (f Filter) normalized() Filter {
	if f.After.IsZero() {
		f.After = beyondTheQueue
	}
	if f.Limit <= 0 {
		f.Limit = defaultQueuePage
	}
	if f.Limit > maxQueuePage {
		f.Limit = maxQueuePage
	}
	return f
}

// Page is one read of the queue, and where the next one starts.
type Page struct {
	// Incidents are the rooms, newest activity first.
	Incidents []Incident
	// Next is where the following page begins, zero when this page reached the
	// end of the queue.
	Next Cursor
}

// Queue returns one page of the rooms a filter selects, newest activity first.
func (s *Store) Queue(ctx context.Context, f Filter) (Page, error) {
	f = f.normalized()
	query, args := queueQuery(f)

	var page Page
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("read incident queue: %w", err)
		}
		// Read-only, so the close itself has nothing to report; rows.Err below is
		// the check that matters.
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			incident, err := scanIncident(rows)
			if err != nil {
				return err
			}
			page.Incidents = append(page.Incidents, incident)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("read incident queue: %w", err)
		}
		return nil
	})
	if err != nil {
		return Page{}, err
	}
	// A short page reached the end of the queue. A full one might not have, and
	// the cursor is what says where to carry on from — including when the queue
	// has moved underneath the reader, which it will have.
	if len(page.Incidents) == f.Limit {
		last := page.Incidents[len(page.Incidents)-1]
		page.Next = Cursor{LastSeen: last.LastSeen, ID: last.ID}
	}
	return page, nil
}

// queueQuery picks the statement the filter is answered from and lays out its
// arguments. The customer predicate is last so both statements share the
// ordering, filtering and paging arguments position for position.
func queueQuery(f Filter) (string, []any) {
	args := []any{
		labels(f.Statuses), labels(f.Severities), f.RuleID, f.AssigneeID, f.DeviceID,
		f.After.LastSeen, f.After.ID, f.Limit,
	}
	if f.OrganizationID == uuid.Nil {
		return queueForTenantSQL, args
	}
	return queueForCustomerSQL, append(args, f.OrganizationID)
}

// labels renders a closed-vocabulary filter as the text array the statement
// matches against, never nil: an empty array is what "narrow on nothing" is
// written as, and it is a value the planner can read rather than a null.
func labels[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

// incidentColumns is the row every incident read returns, in the order the
// statements above select it.
type incidentColumns interface {
	Scan(dest ...any) error
}

// scanIncident reads one room from a row, mapping the columns a room may not
// have yet — nobody working it, no answer for why it ended — onto their zero
// values rather than onto a pointer every caller would have to check.
func scanIncident(row incidentColumns) (Incident, error) {
	var (
		incident   Incident
		assignee   uuid.NullUUID
		resolvedAt sql.NullTime
		cause      sql.NullString
	)
	if err := row.Scan(
		&incident.ID, &incident.OrganizationID, &incident.RuleID, &incident.Scope,
		&incident.ScopeKey, &incident.Severity, &incident.Status, &assignee,
		&incident.OpenedAt, &incident.FirstSeen, &incident.LastSeen, &resolvedAt, &cause,
		&incident.Occurrences, &incident.DeviceCount); err != nil {
		return Incident{}, fmt.Errorf("scan incident: %w", err)
	}
	incident.AssigneeID = assignee.UUID
	incident.ResolvedAt = resolvedAt.Time
	incident.CauseCode = CauseCode(cause.String)
	return incident, nil
}
