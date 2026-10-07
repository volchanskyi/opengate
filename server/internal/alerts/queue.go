package alerts

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const (
	defaultQueuePage = 50
	// A caller asking for the whole table is answered with a page and the cursor after it.
	maxQueuePage = 200
)

// scopeNameColumn names the host, site or customer a room is about; it is evaluated per returned
// row, so the page's indexed read keeps its shape, and a removed record reads as null.
const scopeNameColumn = `
	       CASE i.scope
	         WHEN 'device' THEN (SELECT d.hostname FROM devices d
	                              WHERE d.id = i.scope_key AND d.tenant_id = i.tenant_id)
	         WHEN 'site' THEN (SELECT s.name FROM sites s
	                            WHERE s.id = i.scope_key AND s.tenant_id = i.tenant_id)
	         WHEN 'organization' THEN (SELECT o.name FROM organizations o
	                                    WHERE o.id = i.scope_key AND o.tenant_id = i.tenant_id)
	       END`

// Every filter is a sentinel comparison, so the statement keeps one shape for the customer index.
// The all-zero uuid means not narrowing on that filter.
const queueForCustomerSQL = `
	SELECT i.id, i.organization_id, i.rule_id, i.scope, i.scope_key, i.severity, i.status,
	       i.assignee_id, i.opened_at, i.first_seen, i.last_seen, i.resolved_at, i.cause_code,
	       i.occurrences, i.device_count,` + scopeNameColumn + `
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

// A separate statement: customer-first ordering would sort the whole table across customers.
const queueForTenantSQL = `
	SELECT i.id, i.organization_id, i.rule_id, i.scope, i.scope_key, i.severity, i.status,
	       i.assignee_id, i.opened_at, i.first_seen, i.last_seen, i.resolved_at, i.cause_code,
	       i.occurrences, i.device_count,` + scopeNameColumn + `
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

// Cursor is where a page ended: last activity plus id, which together order the queue uniquely.
type Cursor struct {
	LastSeen time.Time
	ID       uuid.UUID
}

// IsZero reports whether the cursor names no position.
func (c Cursor) IsZero() bool { return c.ID == uuid.Nil }

// A first page starts later than any moment a room can be seen.
var beyondTheQueue = Cursor{
	LastSeen: time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC),
	ID:       uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff"),
}

// Filter selects queue rooms; a field left at its zero value narrows nothing.
type Filter struct {
	// OrganizationID narrows to one customer; every member of a tenant may see every customer in it.
	OrganizationID uuid.UUID
	Statuses       []Status
	Severities     []Severity
	RuleID         string
	// DeviceID narrows to rooms holding an alert that machine raised; a room spans many machines.
	DeviceID   uuid.UUID
	AssigneeID uuid.UUID
	After      Cursor
	Limit      int
}

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
	Incidents []Incident
	// Next is zero when this page reached the end of the queue.
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
		// Read-only, so the close has nothing to report; rows.Err below is the check.
		defer func() { _ = rows.Close() }()

		for rows.Next() {
			incident, err := scanNamedIncident(rows)
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
	// A short page reached the end of the queue; a full one hands back a cursor to carry on from.
	if len(page.Incidents) == f.Limit {
		last := page.Incidents[len(page.Incidents)-1]
		page.Next = Cursor{LastSeen: last.LastSeen, ID: last.ID}
	}
	return page, nil
}

// The customer argument is last so both statements share the other arguments positionally.
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

// labels renders a filter as a text array, never nil: an empty array means narrow on nothing.
func labels[T ~string](values []T) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	return out
}

type incidentColumns interface {
	Scan(dest ...any) error
}

// Columns a room may lack, an assignee or a cause, map to zero values so callers check no pointer.
// Columns a statement selects after the incident's own are scanned into extra.
func scanIncident(row incidentColumns, extra ...any) (Incident, error) {
	var (
		incident   Incident
		assignee   uuid.NullUUID
		resolvedAt sql.NullTime
		cause      sql.NullString
	)
	dest := append([]any{
		&incident.ID, &incident.OrganizationID, &incident.RuleID, &incident.Scope,
		&incident.ScopeKey, &incident.Severity, &incident.Status, &assignee,
		&incident.OpenedAt, &incident.FirstSeen, &incident.LastSeen, &resolvedAt, &cause,
		&incident.Occurrences, &incident.DeviceCount}, extra...)
	if err := row.Scan(dest...); err != nil {
		return Incident{}, fmt.Errorf("scan incident: %w", err)
	}
	incident.AssigneeID = assignee.UUID
	incident.ResolvedAt = resolvedAt.Time
	incident.CauseCode = CauseCode(cause.String)
	return incident, nil
}

// scanNamedIncident reads an incident followed by the name of what it is about.
func scanNamedIncident(row incidentColumns) (Incident, error) {
	var name sql.NullString
	incident, err := scanIncident(row, &name)
	incident.ScopeName = name.String
	return incident, err
}
