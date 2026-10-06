package alerts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
)

const (
	// maxRoomAlerts is how many of a room's alerts one read returns, newest first.
	maxRoomAlerts = 200
	// maxRoomEvents is the same bound on a room's history.
	maxRoomEvents = 200
	// maxCommentBytes is the most one comment may weigh.
	maxCommentBytes = 4096
)

// roomSQL reads one room; a room outside the caller's customer or tenant answers "no such room".
const roomSQL = `
	SELECT id, organization_id, rule_id, scope, scope_key, severity, status,
	       assignee_id, opened_at, first_seen, last_seen, resolved_at, cause_code,
	       occurrences, device_count
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND id = $1
	   AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR organization_id = $2::uuid)`

// roomAlertsSQL lists what folded into a room, newest first, with the total on every row.
// The evidence column is selected only as presence and size.
const roomAlertsSQL = `
	SELECT a.id, a.device_id, a.rule_id, a.rule_version, a.severity, a.metric, a.value,
	       a.window_start, a.window_end, a.observed_at, a.received_at, a.backfilled,
	       a.evidence_codec, COALESCE(length(a.evidence), 0), COUNT(*) OVER ()
	  FROM alerts a
	 WHERE a.tenant_id = current_setting('app.current_tenant')::uuid
	   AND a.incident_id = $1
	 ORDER BY a.observed_at DESC, a.id DESC
	 LIMIT $2::integer`

// roomEventsSQL reads the newest lines of a room's history; the caller restores their order.
const roomEventsSQL = `
	SELECT id, at, kind, actor_id, body, COUNT(*) OVER ()
	  FROM incident_events
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND incident_id = $1
	 ORDER BY at DESC, id DESC
	 LIMIT $2::integer`

// assignRoomSQL records who is working a room; an empty assignee hands it back to the queue.
const assignRoomSQL = `
	UPDATE incidents SET assignee_id = NULLIF($2::text, '')::uuid
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

// alertEvidenceSQL reads one alert's evidence through the room holding it, so an alert id
// alone never reaches another tenant's incident.
const alertEvidenceSQL = `
	SELECT evidence, evidence_codec FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND id = $1 AND incident_id = $2`

// FoldedAlert is one alert as its room lists it, with evidence size but not the evidence.
type FoldedAlert struct {
	// ID names the alert, and is what an evidence read asks for.
	ID uuid.UUID
	// DeviceID is the machine that raised it.
	DeviceID uuid.UUID
	// RuleID and RuleVersion name the rule as it stood when it fired.
	RuleID      string
	RuleVersion uint32
	// Severity is this reading's severity; the room carries the worst of what folded in.
	Severity Severity
	// Metric and Value are the dimension and reading that crossed; both are absent for event rules.
	Metric string
	Value  *float64
	// WindowStart, WindowEnd and ObservedAt are event time on the machine; ReceivedAt is server time.
	WindowStart time.Time
	WindowEnd   time.Time
	ObservedAt  time.Time
	ReceivedAt  time.Time
	// Backfilled marks a finding a retroactive scan produced over local history.
	Backfilled bool
	// EvidenceCodec names how the evidence is compressed, empty when there is none.
	// EvidenceBytes is its compressed size.
	EvidenceCodec string
	EvidenceBytes int
}

// Event is one line of a room's history — what happened, when, and who did it.
type Event struct {
	// ID names the line.
	ID uuid.UUID
	// At is when it happened.
	At time.Time
	// Kind is what sort of line it is, from the closed set the database keeps.
	Kind string
	// ActorID is who did it, zero when the system did.
	ActorID uuid.UUID
	// Body is what the line says, in the shape its kind defines.
	Body json.RawMessage
}

// Investigation is the whole of one room, as somebody opening it sees it.
type Investigation struct {
	// Incident is where the room stands.
	Incident Incident
	// Alerts are the newest of what folded in, and AlertsTotal how many there are.
	Alerts      []FoldedAlert
	AlertsTotal int
	// Events are the room's history in chronological order, and EventsTotal its length.
	Events      []Event
	EventsTotal int
}

// Investigation returns one room with its alerts and timeline; a zero organizationID does not
// narrow, and a room outside the customer or tenant answers as absent.
func (s *Store) Investigation(ctx context.Context, incidentID, organizationID uuid.UUID) (Investigation, error) {
	var room Investigation
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		incident, err := readRoom(ctx, tx, incidentID, organizationID)
		if err != nil {
			return err
		}
		room.Incident = incident

		if room.Alerts, room.AlertsTotal, err = readRoomAlerts(ctx, tx, incidentID); err != nil {
			return err
		}
		room.Events, room.EventsTotal, err = readRoomEvents(ctx, tx, incidentID)
		return err
	})
	if err != nil {
		return Investigation{}, err
	}
	return room, nil
}

// Incident reads where one room stands, without alerts or history; a room outside the customer
// or tenant answers as absent, so moves resolve the room first.
func (s *Store) Incident(ctx context.Context, incidentID, organizationID uuid.UUID) (Incident, error) {
	var incident Incident
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		var err error
		incident, err = readRoom(ctx, tx, incidentID, organizationID)
		return err
	})
	if err != nil {
		return Incident{}, err
	}
	return incident, nil
}

// readRoom reads one room inside an open transaction; a foreign tenant or customer reads as absent.
func readRoom(ctx context.Context, tx *sql.Tx, incidentID, organizationID uuid.UUID) (Incident, error) {
	incident, err := scanIncident(tx.QueryRowContext(ctx, roomSQL, incidentID, organizationID))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Incident{}, fmt.Errorf("%w: %s", ErrIncidentNotFound, incidentID)
	case err != nil:
		return Incident{}, fmt.Errorf("read incident: %w", err)
	}
	return incident, nil
}

// readRoomAlerts reads the newest alerts folded into a room and the total folded in.
func readRoomAlerts(ctx context.Context, tx *sql.Tx, incidentID uuid.UUID) ([]FoldedAlert, int, error) {
	rows, err := tx.QueryContext(ctx, roomAlertsSQL, incidentID, maxRoomAlerts)
	if err != nil {
		return nil, 0, fmt.Errorf("read incident alerts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		folded []FoldedAlert
		total  int
	)
	for rows.Next() {
		var alert FoldedAlert
		if err := rows.Scan(&alert.ID, &alert.DeviceID, &alert.RuleID, &alert.RuleVersion,
			&alert.Severity, &alert.Metric, &alert.Value, &alert.WindowStart, &alert.WindowEnd,
			&alert.ObservedAt, &alert.ReceivedAt, &alert.Backfilled,
			&alert.EvidenceCodec, &alert.EvidenceBytes, &total); err != nil {
			return nil, 0, fmt.Errorf("scan incident alert: %w", err)
		}
		folded = append(folded, alert)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read incident alerts: %w", err)
	}
	return folded, total, nil
}

// readRoomEvents reads a room's newest history lines and returns them in chronological order.
func readRoomEvents(ctx context.Context, tx *sql.Tx, incidentID uuid.UUID) ([]Event, int, error) {
	rows, err := tx.QueryContext(ctx, roomEventsSQL, incidentID, maxRoomEvents)
	if err != nil {
		return nil, 0, fmt.Errorf("read incident events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var (
		events []Event
		total  int
	)
	for rows.Next() {
		var (
			event Event
			actor uuid.NullUUID
		)
		if err := rows.Scan(&event.ID, &event.At, &event.Kind, &actor, &event.Body, &total); err != nil {
			return nil, 0, fmt.Errorf("scan incident event: %w", err)
		}
		event.ActorID = actor.UUID
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read incident events: %w", err)
	}
	reverse(events)
	return events, total, nil
}

// reverse turns a newest-first read into chronological order.
func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}

// Assign records who is working a room in the column the queue filters on and in its history.
// A zero assignee hands the room back to the queue.
func (s *Store) Assign(ctx context.Context, incidentID, assignee, actor uuid.UUID) error {
	at := s.now().UTC().Truncate(time.Microsecond)
	body, err := json.Marshal(assignmentBody{
		AssigneeID: actorArg(assignee),
		Unassigned: assignee == uuid.Nil,
	})
	if err != nil {
		return fmt.Errorf("encode incident assignment: %w", err)
	}

	return dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, _, err := roomUnderChange(ctx, tx, incidentID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, assignRoomSQL, incidentID, actorArg(assignee)); err != nil {
			return fmt.Errorf("assign incident: %w", err)
		}
		if _, err := tx.ExecContext(ctx, appendRoomEventSQL,
			incidentID, at, kindAssignment, actorArg(actor), body, uuid.New()); err != nil {
			return fmt.Errorf("record incident assignment: %w", err)
		}
		return nil
	})
}

// Comment adds one person's note to a room's append-only history.
func (s *Store) Comment(ctx context.Context, incidentID, actor uuid.UUID, note string) (Event, error) {
	note = strings.TrimSpace(note)
	if note == "" || len(note) > maxCommentBytes {
		return Event{}, fmt.Errorf("%w: %d bytes", ErrCommentUnusable, len(note))
	}
	body, err := json.Marshal(commentBody{Body: note})
	if err != nil {
		return Event{}, fmt.Errorf("encode incident comment: %w", err)
	}

	event := Event{
		ID:      uuid.New(),
		At:      s.now().UTC().Truncate(time.Microsecond),
		Kind:    kindComment,
		ActorID: actor,
		Body:    body,
	}
	err = dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		if _, _, err := roomUnderChange(ctx, tx, incidentID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, appendRoomEventSQL,
			incidentID, event.At, event.Kind, actorArg(actor), body, event.ID); err != nil {
			return fmt.Errorf("record incident comment: %w", err)
		}
		return nil
	})
	if err != nil {
		return Event{}, err
	}
	return event, nil
}

// Evidence returns one alert's compressed evidence bytes and its codec, read through the room
// holding the alert.
func (s *Store) Evidence(ctx context.Context, incidentID, alertID uuid.UUID) ([]byte, string, error) {
	var (
		blob  []byte
		codec string
	)
	err := dbtx.Scoped(ctx, s.db, func(tx *sql.Tx) error {
		switch err := tx.QueryRowContext(ctx, alertEvidenceSQL, alertID, incidentID).
			Scan(&blob, &codec); {
		case errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("%w: %s", ErrAlertNotFound, alertID)
		case err != nil:
			return fmt.Errorf("read alert evidence: %w", err)
		}
		if len(blob) == 0 {
			return fmt.Errorf("%w: %s", ErrNoEvidence, alertID)
		}
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return blob, codec, nil
}

// Kinds of line a person's move puts in a room's history.
const (
	kindAssignment = "assignment"
	kindComment    = "comment"
)

// assignmentBody is what an assignment line says; handing a room back is stated outright.
type assignmentBody struct {
	AssigneeID string `json:"assignee_id,omitempty"`
	Unassigned bool   `json:"unassigned,omitempty"`
}

// commentBody is what a comment line says.
type commentBody struct {
	Body string `json:"body"`
}
