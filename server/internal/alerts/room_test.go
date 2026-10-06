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

func TestRoomTimelineReadsForwards(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	id := e.openRoomAt(t, StatusNew, e.now)

	// Each move lands at its own moment, since a stopped clock would leave the order to the ids.
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

func TestRoomNeverCarriesAnEvidenceBlob(t *testing.T) {
	t.Parallel()
	bytes := reflect.TypeFor[[]byte]()
	for field := range reflect.TypeFor[FoldedAlert]().Fields() {
		assert.NotEqualf(t, bytes, field.Type,
			"FoldedAlert.%s carries bytes — a room must not embed evidence", field.Name)
	}
}

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

func TestRoomStopsAtTheTenantWallAndAtTheCustomer(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	next := e.neighbour(t, "Northwind")
	fabrikam := testutil.SeedOrganization(t, e.ctx, e.store, "Fabrikam")

	theirs := e.openRoomIn(t, next, StatusNew, e.now)
	ours := e.openRoomAt(t, StatusNew, e.now)

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

func (e estate) opened(t *testing.T, id uuid.UUID) Investigation {
	t.Helper()
	room, err := e.alerts.Investigation(e.ctx, id, uuid.Nil)
	require.NoError(t, err)
	return room
}

func kinds(room Investigation) []string {
	out := make([]string, 0, len(room.Events))
	for _, event := range room.Events {
		out = append(out, event.Kind)
	}
	return out
}

func (e estate) clockAt(when time.Time) {
	e.alerts.now = func() time.Time { return when }
}

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

func (e estate) seedRoomEvents(t *testing.T, incidentID uuid.UUID, n int, actor uuid.UUID) {
	t.Helper()
	e.exec(t,
		`INSERT INTO incident_events (id, tenant_id, organization_id, incident_id, at, kind, actor_id, body)
		 SELECT gen_random_uuid(), $1, $2, $3, $4::timestamptz + make_interval(secs => g),
		        'comment', $5, '{"body": "note"}'::jsonb
		   FROM generate_series(1, $6) AS g`,
		e.tenant, e.org, incidentID, e.now.Add(-time.Hour), actor, n)
}
