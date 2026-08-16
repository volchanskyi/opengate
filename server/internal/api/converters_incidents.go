package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/volchanskyi/opengate/server/internal/alerts"
)

// Turning the investigation store's answers into the wire shapes, and the
// wire's questions back into filters.
//
// The cursor is the one that carries a design decision rather than a mapping.
// It is opaque to the caller on purpose: it names a position in the queue, and
// a client that could read it would start constructing positions, which is how
// a paging contract becomes an unindexed query somebody else has to support.

// errUnreadableCursor is a position that names nothing. It is refused rather
// than ignored — silently starting from the top would hand a technician the
// first page again while they believed they were reading the second.
var errUnreadableCursor = errors.New("cursor does not name a position in the queue")

// encodeCursor renders where a page ended, empty when the page reached the end
// of the queue.
func encodeCursor(cursor alerts.Cursor) string {
	if cursor.IsZero() {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(
		[]byte(cursor.LastSeen.UTC().Format(time.RFC3339Nano) + "|" + cursor.ID.String()))
}

// decodeCursor reads a position back.
func decodeCursor(encoded string) (alerts.Cursor, error) {
	if encoded == "" {
		return alerts.Cursor{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return alerts.Cursor{}, errUnreadableCursor
	}
	at, id, found := strings.Cut(string(raw), "|")
	if !found {
		return alerts.Cursor{}, errUnreadableCursor
	}
	lastSeen, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return alerts.Cursor{}, errUnreadableCursor
	}
	position, err := uuid.Parse(id)
	if err != nil {
		return alerts.Cursor{}, errUnreadableCursor
	}
	return alerts.Cursor{LastSeen: lastSeen, ID: position}, nil
}

// incidentFilterFromParams turns the query string into the store's filter. An
// absent parameter leaves its field zero, which does not narrow.
func incidentFilterFromParams(params ListInvestigationsParams) (alerts.Filter, error) {
	cursor, err := decodeCursor(deref(params.Cursor))
	if err != nil {
		return alerts.Filter{}, err
	}
	filter := alerts.Filter{
		RuleID: deref(params.RuleId),
		After:  cursor,
		Limit:  deref(params.Limit),
	}
	assignUUID(&filter.OrganizationID, params.OrganizationId)
	assignUUID(&filter.DeviceID, params.DeviceId)
	assignUUID(&filter.AssigneeID, params.AssigneeId)
	if params.Status != nil {
		filter.Statuses = mapped[IncidentStatus, alerts.Status](*params.Status)
	}
	if params.Severity != nil {
		filter.Severities = mapped[IncidentSeverity, alerts.Severity](*params.Severity)
	}
	return filter, nil
}

// deref reads an optional parameter, giving the zero value when it is absent.
func deref[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}

// assignUUID fills in an optional identifier, leaving it zero when absent.
func assignUUID(into *uuid.UUID, value *uuid.UUID) {
	if value != nil {
		*into = *value
	}
}

// mapped converts a closed vocabulary from the wire's spelling to the store's.
// Both are the same set of strings; the two types exist so a value from one
// side cannot be passed where the other is meant.
func mapped[From ~string, To ~string](values []From) []To {
	out := make([]To, 0, len(values))
	for _, value := range values {
		out = append(out, To(value))
	}
	return out
}

// incidentPageToAPI renders one read of the queue, and where the next one
// starts.
func incidentPageToAPI(page alerts.Page) IncidentPage {
	items := make([]Incident, 0, len(page.Incidents))
	for _, incident := range page.Incidents {
		items = append(items, incidentToAPI(incident))
	}
	out := IncidentPage{Items: items}
	if cursor := encodeCursor(page.Next); cursor != "" {
		out.NextCursor = &cursor
	}
	return out
}

// incidentToAPI renders one room. The fields a room may not have yet — nobody
// working it, no answer for why it ended — are absent rather than zero, so a
// reader is never shown an assignee of all zeros.
func incidentToAPI(incident alerts.Incident) Incident {
	out := Incident{
		Id:             incident.ID,
		OrganizationId: incident.OrganizationID,
		RuleId:         incident.RuleID,
		Scope:          IncidentScope(incident.Scope),
		ScopeKey:       incident.ScopeKey,
		Severity:       IncidentSeverity(incident.Severity),
		Status:         IncidentStatus(incident.Status),
		OpenedAt:       incident.OpenedAt,
		FirstSeen:      incident.FirstSeen,
		LastSeen:       incident.LastSeen,
		Occurrences:    incident.Occurrences,
		DeviceCount:    incident.DeviceCount,
	}
	if incident.AssigneeID != uuid.Nil {
		assignee := incident.AssigneeID
		out.AssigneeId = &assignee
	}
	if incident.CauseCode != "" {
		cause := IncidentCauseCode(incident.CauseCode)
		out.CauseCode = &cause
	}
	if !incident.ResolvedAt.IsZero() {
		resolvedAt := incident.ResolvedAt
		out.ResolvedAt = &resolvedAt
	}
	return out
}

// investigationToAPI renders the whole of one room: where it stands, the most
// recent of what folded in, and the history in the order it happened. Both
// lists carry their totals, so a bounded page is visibly a page.
func investigationToAPI(room alerts.Investigation) IncidentDetail {
	folded := make([]IncidentAlert, 0, len(room.Alerts))
	for _, alert := range room.Alerts {
		folded = append(folded, foldedAlertToAPI(alert))
	}
	events := make([]IncidentEvent, 0, len(room.Events))
	for _, event := range room.Events {
		events = append(events, incidentEventToAPI(event))
	}
	return IncidentDetail{
		Incident:    incidentToAPI(room.Incident),
		Alerts:      folded,
		AlertsTotal: room.AlertsTotal,
		Events:      events,
		EventsTotal: room.EventsTotal,
	}
}

// foldedAlertToAPI renders one alert as its room lists it — what evidence
// exists and what fetching it costs, never the evidence itself.
func foldedAlertToAPI(alert alerts.FoldedAlert) IncidentAlert {
	out := IncidentAlert{
		Id:            alert.ID,
		DeviceId:      alert.DeviceID,
		RuleId:        alert.RuleID,
		RuleVersion:   int(alert.RuleVersion),
		Severity:      IncidentSeverity(alert.Severity),
		Value:         alert.Value,
		WindowStart:   alert.WindowStart,
		WindowEnd:     alert.WindowEnd,
		ObservedAt:    alert.ObservedAt,
		ReceivedAt:    alert.ReceivedAt,
		Backfilled:    alert.Backfilled,
		EvidenceBytes: alert.EvidenceBytes,
	}
	if alert.Metric != "" {
		metric := alert.Metric
		out.Metric = &metric
	}
	if alert.EvidenceCodec != "" {
		codec := alert.EvidenceCodec
		out.EvidenceCodec = &codec
	}
	return out
}

// incidentEventToAPI renders one line of a room's history. A body that cannot
// be read comes back empty rather than failing the whole timeline: one
// unreadable line must not cost a technician the handover it sits in.
func incidentEventToAPI(event alerts.Event) IncidentEvent {
	body := map[string]any{}
	if len(event.Body) > 0 {
		_ = json.Unmarshal(event.Body, &body)
	}
	out := IncidentEvent{
		Id:   event.ID,
		At:   event.At,
		Kind: IncidentEventKind(event.Kind),
		Body: body,
	}
	if event.ActorID != uuid.Nil {
		actor := event.ActorID
		out.ActorId = &actor
	}
	return out
}
