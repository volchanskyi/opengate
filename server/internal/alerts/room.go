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

// One room, opened: where it stands, what folded into it, and what people did
// about it.
//
// Evidence is deliberately not part of that. It is tens of kilobytes per alert
// and a fleet event folds hundreds of them, so a room that carried its evidence
// would move megabytes to render a page nobody has scrolled. The room says which
// alerts have evidence and how much of it there is; fetching one is a call of
// its own.
//
// Both lists are bounded and both say what they are a page of. A bound with no
// total is the shape that quietly turns "312 alerts across 40 machines" into
// "200 alerts", which is a different incident.

const (
	// maxRoomAlerts is how many of a room's alerts one read returns, newest
	// first. Contoso's 02:41 driver rollout is 312 rows in one room, and the
	// difference between the 200th and the 312th is not what anybody opens it
	// for — the count is.
	maxRoomAlerts = 200
	// maxRoomEvents is the same bound on a room's history.
	maxRoomEvents = 200
	// maxCommentBytes is the most one comment may weigh. A handover note is
	// prose; without a bound a person's text box decides how much a row costs.
	maxCommentBytes = 4096
)

// roomSQL reads one room, refusing a room outside the customer the caller is
// looking at exactly as it refuses one outside the tenant. Both are answered
// "no such room": a caller must not be able to tell a room they may not see from
// one that does not exist.
const roomSQL = `
	SELECT id, organization_id, rule_id, scope, scope_key, severity, status,
	       assignee_id, opened_at, first_seen, last_seen, resolved_at, cause_code,
	       occurrences, device_count
	  FROM incidents
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND id = $1
	   AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR organization_id = $2::uuid)`

// roomAlertsSQL lists what folded into a room, newest first, and how many there
// are in total.
//
// The evidence column is never selected — only whether there is one and how big
// it is. The count rides on every row so one pass answers both, rather than a
// second statement counting a set that may have changed in between.
const roomAlertsSQL = `
	SELECT a.id, a.device_id, a.rule_id, a.rule_version, a.severity, a.metric, a.value,
	       a.window_start, a.window_end, a.observed_at, a.received_at, a.backfilled,
	       a.evidence_codec, COALESCE(length(a.evidence), 0), COUNT(*) OVER ()
	  FROM alerts a
	 WHERE a.tenant_id = current_setting('app.current_tenant')::uuid
	   AND a.incident_id = $1
	 ORDER BY a.observed_at DESC, a.id DESC
	 LIMIT $2::integer`

// roomEventsSQL reads the newest lines of a room's history. Newest, because a
// bound has to keep the part a handover is about; the caller turns them back
// into the order they happened.
const roomEventsSQL = `
	SELECT id, at, kind, actor_id, body, COUNT(*) OVER ()
	  FROM incident_events
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND incident_id = $1
	 ORDER BY at DESC, id DESC
	 LIMIT $2::integer`

// assignRoomSQL records who is working a room. Empty hands it back to the
// queue, which is a move a technician going off shift has to be able to make.
const assignRoomSQL = `
	UPDATE incidents SET assignee_id = NULLIF($2::text, '')::uuid
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid AND id = $1`

// alertEvidenceSQL reads one alert's frozen evidence, through the room holding
// it. The room is part of the key rather than a check afterwards: an alert id on
// its own is a guessable handle to somebody else's incident.
const alertEvidenceSQL = `
	SELECT evidence, evidence_codec FROM alerts
	 WHERE tenant_id = current_setting('app.current_tenant')::uuid
	   AND id = $1 AND incident_id = $2`

// FoldedAlert is one alert as its room lists it: everything a technician reads
// off the list, and no evidence. Carrying the blob here is what turns a room
// into megabytes, so what is carried instead is enough to decide whether to
// fetch one.
type FoldedAlert struct {
	// ID names the alert, and is what an evidence read asks for.
	ID uuid.UUID
	// DeviceID is the machine that raised it.
	DeviceID uuid.UUID
	// RuleID and RuleVersion name the rule as it stood when it fired.
	RuleID      string
	RuleVersion uint32
	// Severity is how bad this one reading was, which can be less than the
	// room's: the room carries the worst of what folded in.
	Severity Severity
	// Metric and Value are the dimension and the reading that crossed, both
	// absent for a rule that fires on an event rather than a number.
	Metric string
	Value  *float64
	// WindowStart, WindowEnd and ObservedAt are event time — when it happened on
	// the machine. ReceivedAt is when the server heard, which for a retroactive
	// finding can be months later.
	WindowStart time.Time
	WindowEnd   time.Time
	ObservedAt  time.Time
	ReceivedAt  time.Time
	// Backfilled marks a finding a retroactive scan produced over local history.
	Backfilled bool
	// EvidenceCodec names how this alert's evidence is compressed, empty when it
	// carries none. EvidenceBytes is its compressed weight, so a reader knows
	// what a fetch costs before making it.
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
	// ActorID is who did it, zero when the system did — which is how an
	// auto-resolution is told apart from somebody's decision.
	ActorID uuid.UUID
	// Body is what the line says, in the shape its kind defines.
	Body json.RawMessage
}

// Investigation is the whole of one room, as somebody opening it sees it.
type Investigation struct {
	// Incident is where the room stands.
	Incident Incident
	// Alerts are the newest of what folded in, and AlertsTotal how many there
	// are — a bounded page that says what it is a page of.
	Alerts      []FoldedAlert
	AlertsTotal int
	// Events are the room's history in the order it happened, and EventsTotal
	// how long that history is.
	Events      []Event
	EventsTotal int
}

// Investigation returns one room with its alerts and its timeline.
//
// organizationID narrows to the customer the caller is looking at; zero does not
// narrow. A room outside it answers the same as a room outside the tenant, so
// neither boundary is discoverable by probing ids.
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

// Incident reads where one room stands, without its alerts or its history.
//
// It is both the answer a move hands back and the check a caller makes before
// making one: resolving the room first is what keeps acting on a room outside
// the customer on screen — or outside the tenant — from being possible, and
// both refuse identically so neither boundary is discoverable by probing ids.
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

// readRoom reads one room inside an open transaction, refusing a room outside
// the tenant and one outside the customer the caller is looking at the same way.
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

// readRoomAlerts reads the newest of what folded into a room, and how much
// folded in altogether.
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

// readRoomEvents reads a room's history and hands it back in the order it
// happened. The read is newest-first because that is the half a bound has to
// keep; a timeline is read forwards, so it is turned around here rather than by
// every caller.
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

// reverse turns a newest-first read back into the order things happened.
func reverse(events []Event) {
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
}

// Assign records who is working a room, and says so in its history. Both,
// because they answer different questions: the column is what the queue filters
// on, and the line is what says when it changed hands.
//
// A zero assignee hands the room back to the queue. A technician going off shift
// has to be able to put a room down rather than leave it looking worked.
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

// Comment adds one person's note to a room's history.
//
// A comment is not a field on the incident — it is one more thing that happened,
// in the order it happened, which is why it lands in the same append-only
// history a status change does.
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

// Evidence returns one alert's frozen evidence and the codec naming how it is
// compressed, read through the room holding the alert.
//
// It is bytes rather than a decoded structure because this layer stores what
// arrived and nothing more: the blob is immutable, unfetchable and exactly what
// the machine sent, and deciding what it means belongs to whoever knows the
// codec.
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

// assignmentBody is what an assignment line says. Handing a room back is stated
// outright rather than left as an absent assignee, so a timeline reads "put
// down" instead of a blank.
type assignmentBody struct {
	AssigneeID string `json:"assignee_id,omitempty"`
	Unassigned bool   `json:"unassigned,omitempty"`
}

// commentBody is what a comment line says.
type commentBody struct {
	Body string `json:"body"`
}
