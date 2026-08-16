package alerts

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// What one room has to answer when somebody opens it.
//
// The room is the whole surface an investigation happens on, so what it carries
// decides what can be worked out about an event: the alerts that folded in, the
// timeline of what people did, and — separately, on request — the frozen
// evidence one of those alerts arrived with. Separately because evidence is tens
// of kilobytes per alert and a room holds hundreds of them; a room that carried
// its evidence would move megabytes to render a page nobody has scrolled yet.

// TestRoomCarriesItsAlertsAndItsTimeline is the detail read. Both halves are
// needed and neither substitutes for the other: the alerts say what the machines
// reported, and the timeline says what people did about it, which is what a
// handover between two technicians reads.
func TestRoomCarriesItsAlertsAndItsTimeline(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)

	e.recordUnder(t, e.variant(nil), perCustomer, Stored)
	id := e.roomFor(t, perCustomer, "disk-critical", e.org).ID
	require.NoError(t, e.alerts.Transition(e.ctx, id, Change{To: StatusAcknowledged, Actor: user.ID}))

	room, err := e.alerts.Investigation(e.ctx, id, e.org)
	require.NoError(t, err)

	assert.Equal(t, id, room.Incident.ID)
	assert.Equal(t, StatusAcknowledged, room.Incident.Status)

	require.Len(t, room.Alerts, 1)
	assert.Equal(t, 1, room.AlertsTotal)
	folded := room.Alerts[0]
	assert.Equal(t, e.device, folded.DeviceID)
	assert.Equal(t, "disk-critical", folded.RuleID)
	assert.Equal(t, SeverityCritical, folded.Severity)
	assert.Equal(t, "disk.used_percent", folded.Metric)
	require.NotNil(t, folded.Value)
	assert.InDelta(t, 98.2, *folded.Value, 0.001)
	assert.Equal(t, "deflate-1", folded.EvidenceCodec)
	assert.Positive(t, folded.EvidenceBytes, "a room says how much evidence there is to fetch")

	require.Len(t, room.Events, 1)
	assert.Equal(t, "status_change", room.Events[0].Kind)
	assert.Equal(t, user.ID, room.Events[0].ActorID)
	assert.Contains(t, string(room.Events[0].Body), "acknowledged")
}

// TestRoomTimelineReadsForwards pins the order. A timeline is read the way it
// happened — a handover starts at what opened the room, not at the last thing
// anybody typed.
func TestRoomTimelineReadsForwards(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)

	// Each move at its own moment, because that is what a timeline is ordered by
	// and a stopped clock would leave the order to the ids.
	e.clockAt(e.now.Add(time.Minute))
	require.NoError(t, e.alerts.Transition(e.ctx, id, Change{To: StatusAcknowledged, Actor: user.ID}))
	e.clockAt(e.now.Add(2 * time.Minute))
	_, err := e.alerts.Comment(e.ctx, id, user.ID, "rebooting the array controller")
	require.NoError(t, err)
	e.clockAt(e.now.Add(3 * time.Minute))
	require.NoError(t, e.alerts.Transition(e.ctx, id,
		Change{To: StatusResolved, Cause: CauseFixedByTech, Actor: user.ID}))

	assert.Equal(t, []string{"status_change", "comment", "resolution"}, kinds(e.opened(t, id)))
}

// TestRoomNeverCarriesAnEvidenceBlob is the bound that keeps a room readable.
// Evidence is tens of kilobytes per alert and a fleet event folds hundreds of
// them, so the room reports what evidence exists and how big it is, and fetching
// one is a call of its own.
func TestRoomNeverCarriesAnEvidenceBlob(t *testing.T) {
	t.Parallel()
	bytes := reflect.TypeFor[[]byte]()
	for field := range reflect.TypeFor[FoldedAlert]().Fields() {
		assert.NotEqualf(t, bytes, field.Type,
			"FoldedAlert.%s carries bytes — a room must not embed evidence", field.Name)
	}
}

// TestRoomBoundsWhatItReturns keeps Contoso's 02:41 driver rollout — 312 alerts
// in one room — from being 312 rows in one response. The room says how many
// there are, so a bounded page is visibly a page rather than the whole of it.
func TestRoomBoundsWhatItReturns(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	id := e.openRoomAt(t, StatusNew, e.now)
	e.seedFoldedAlerts(t, id, maxRoomAlerts+12)

	room := e.opened(t, id)
	assert.Len(t, room.Alerts, maxRoomAlerts)
	assert.Equal(t, maxRoomAlerts+12, room.AlertsTotal, "a bounded page still says what it is a page of")
	for i := 1; i < len(room.Alerts); i++ {
		assert.False(t, room.Alerts[i].ObservedAt.After(room.Alerts[i-1].ObservedAt),
			"the alerts kept are the most recent ones")
	}
}

// TestRoomStopsAtTheTenantWallAndAtTheCustomer covers both boundaries with one
// crafted id each. They fail the same way on purpose: a caller must not be able
// to tell a room they may not see from one that does not exist.
func TestRoomStopsAtTheTenantWallAndAtTheCustomer(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	next := e.neighbour(t, "Northwind")
	fabrikam := testutil.SeedOrganization(t, e.ctx, e.store, "Fabrikam")

	theirs := e.openRoomIn(t, next, StatusNew, e.now)
	ours := e.openRoomAt(t, StatusNew, e.now)

	// Reading a room and resolving one before acting on it are the two doors a
	// caller-supplied id comes through, and both refuse the same way whichever
	// boundary is in the way.
	for _, tc := range []struct {
		name string
		id   uuid.UUID
		org  uuid.UUID
	}{
		{"another tenant's room", theirs, uuid.Nil},
		{"another tenant's room while looking at a customer", theirs, e.org},
		{"our room while looking at another customer", ours, fabrikam},
		{"a room that never existed", uuid.New(), uuid.Nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.alerts.Investigation(e.ctx, tc.id, tc.org)
			assert.ErrorIs(t, err, ErrIncidentNotFound)
			_, err = e.alerts.Incident(e.ctx, tc.id, tc.org)
			assert.ErrorIs(t, err, ErrIncidentNotFound)
		})
	}

	// The moves are refused by the wall itself. They carry no customer of their
	// own — the caller resolves the room by reading it first, which is what
	// makes acting on a room outside the customer on screen impossible.
	for _, tc := range []struct {
		name string
		id   uuid.UUID
	}{
		{"another tenant's room", theirs},
		{"a room that never existed", uuid.New()},
	} {
		t.Run("moving "+tc.name, func(t *testing.T) {
			_, err := e.alerts.Comment(e.ctx, tc.id, uuid.Nil, "hello")
			assert.ErrorIs(t, err, ErrIncidentNotFound)
			assert.ErrorIs(t, e.alerts.Assign(e.ctx, tc.id, uuid.Nil, uuid.Nil), ErrIncidentNotFound)
			assert.ErrorIs(t, e.alerts.Transition(e.ctx, tc.id,
				Change{To: StatusAcknowledged}), ErrIncidentNotFound)
		})
	}
}

// is the first move of almost every case here.
func (e estate) opened(t *testing.T, id uuid.UUID) Investigation {
	t.Helper()
	room, err := e.alerts.Investigation(e.ctx, id, uuid.Nil)
	require.NoError(t, err)
	return room
}

// kinds renders a room's history as the sorts of line it holds, which is what
// the order and the presence cases assert on.
func kinds(room Investigation) []string {
	out := make([]string, 0, len(room.Events))
	for _, event := range room.Events {
		out = append(out, event.Kind)
	}
	return out
}

// clockAt moves the store's clock, which is what stamps a move in a room's
// history. A timeline is ordered by when things happened, so a case about order
// has to make its moves at different moments — a stopped clock would leave the
// order to the ids.
func (e estate) clockAt(when time.Time) {
	e.alerts.now = func() time.Time { return when }
}

// seedFoldedAlerts writes n alerts already filed into a room, which is what a
// fleet event looks like by the time anybody opens it.
func (e estate) seedFoldedAlerts(t *testing.T, incidentID uuid.UUID, n int) {
	t.Helper()
	e.exec(t,
		`INSERT INTO alerts (id, tenant_id, organization_id, device_id, rule_id, rule_version,
		                     severity, window_start, window_end, observed_at, received_at,
		                     incident_id)
		 SELECT gen_random_uuid(), $1, $2, $3, 'disk-critical', 1, 'warning',
		        $4::timestamptz - make_interval(secs => g),
		        $4::timestamptz - make_interval(secs => g),
		        $4::timestamptz - make_interval(secs => g),
		        $4::timestamptz, $5
		   FROM generate_series(1, $6) AS g`,
		e.tenant, e.org, e.device, e.now, incidentID, n)
}

// TestRoomEventsAreBounded keeps a long-running room's history from becoming the
// whole response, the same way its alerts are.
func TestRoomEventsAreBounded(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)
	e.seedRoomEvents(t, id, maxRoomEvents+7, user.ID)

	room := e.opened(t, id)
	assert.Len(t, room.Events, maxRoomEvents)
	assert.Equal(t, maxRoomEvents+7, room.EventsTotal)
}

// seedRoomEvents writes n comments into a room's history.
func (e estate) seedRoomEvents(t *testing.T, incidentID uuid.UUID, n int, actor uuid.UUID) {
	t.Helper()
	e.exec(t,
		`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, actor_id, body)
		 SELECT gen_random_uuid(), $1, $2, $3, $4::timestamptz + make_interval(secs => g),
		        'comment', $5, '{"body": "note"}'::jsonb
		   FROM generate_series(1, $6) AS g`,
		e.tenant, e.org, incidentID, e.now.Add(-time.Hour), actor, n)
}
