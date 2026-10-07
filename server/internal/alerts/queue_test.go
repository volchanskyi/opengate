package alerts

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/volchanskyi/opengate/server/internal/dbtx"
	"github.com/volchanskyi/opengate/server/internal/testutil"
)

// room is one incident a queue test seeds, with every field a queue filter can narrow on.
type room struct {
	org      uuid.UUID
	ruleID   string
	severity Severity
	status   Status
	assignee uuid.UUID
	lastSeen time.Time
}

func (e estate) seed(t *testing.T, r room) uuid.UUID {
	t.Helper()
	if r.org == uuid.Nil {
		r.org = e.org
	}
	if r.ruleID == "" {
		r.ruleID = "disk-critical"
	}
	if r.severity == "" {
		r.severity = SeverityWarning
	}
	if r.status == "" {
		r.status = StatusNew
	}
	if r.lastSeen.IsZero() {
		r.lastSeen = e.now
	}
	// Each room has a key of its own, so seeding many never collides on the
	// one-open-room-per-key index.
	id := uuid.New()
	e.exec(t,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, assignee_id, opened_at, first_seen, last_seen,
		                        resolved_at, occurrences, device_count)
		 VALUES ($1::uuid, $2::uuid, $3::uuid, $4::text, 'device', gen_random_uuid(),
		         $5::text, $6::text, NULLIF($7::text, '')::uuid, $8::timestamptz,
		         $8::timestamptz, $8::timestamptz,
		         CASE WHEN $6::text = 'resolved' THEN $8::timestamptz END, 1, 1)`,
		id, e.tenant, r.org, r.ruleID, string(r.severity), string(r.status),
		actorArg(r.assignee), r.lastSeen)
	return id
}

func (e estate) queue(t *testing.T, f Filter) Page {
	t.Helper()
	page, err := e.alerts.Queue(e.ctx, f)
	require.NoError(t, err)
	return page
}

func ids(page Page) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(page.Incidents))
	for _, incident := range page.Incidents {
		out = append(out, incident.ID)
	}
	return out
}

func TestQueueAnswersNewestActivityFirst(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	oldest := e.seed(t, room{lastSeen: e.now.Add(-2 * time.Hour), ruleID: "cpu-saturated"})
	newest := e.seed(t, room{lastSeen: e.now, ruleID: "disk-critical"})
	middle := e.seed(t, room{lastSeen: e.now.Add(-time.Hour), ruleID: "memory-exhausted"})

	page := e.queue(t, Filter{OrganizationID: e.org})

	assert.Equal(t, []uuid.UUID{newest, middle, oldest}, ids(page))
	assert.True(t, page.Next.IsZero(), "a page that exhausted the queue offers no cursor")
}

func TestQueueCarriesWhatTheQueueIsReadFor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)

	id := e.seed(t, room{
		ruleID: "disk-critical", severity: SeverityCritical,
		status: StatusInvestigating, assignee: user.ID, lastSeen: e.now,
	})

	page := e.queue(t, Filter{OrganizationID: e.org})

	require.Len(t, page.Incidents, 1)
	got := page.Incidents[0]
	assert.Equal(t, id, got.ID)
	assert.Equal(t, e.org, got.OrganizationID)
	assert.Equal(t, "disk-critical", got.RuleID)
	assert.Equal(t, ScopeDevice, got.Scope)
	assert.Equal(t, SeverityCritical, got.Severity)
	assert.Equal(t, StatusInvestigating, got.Status)
	assert.Equal(t, user.ID, got.AssigneeID)
	assert.Equal(t, e.now, got.LastSeen.UTC())
	assert.Equal(t, 1, got.Occurrences)
	assert.Equal(t, 1, got.DeviceCount)
}

func TestQueueNarrowsOnEveryAxisTheUICanOffer(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)

	wanted := e.seed(t, room{
		ruleID: "disk-critical", severity: SeverityCritical,
		status: StatusAcknowledged, assignee: user.ID, lastSeen: e.now,
	})
	// One neighbour per axis, each differing from the wanted room only in the filtered field.
	e.seed(t, room{ruleID: "cpu-saturated", severity: SeverityCritical, status: StatusAcknowledged, assignee: user.ID})
	e.seed(t, room{ruleID: "disk-critical", severity: SeverityInfo, status: StatusAcknowledged, assignee: user.ID})
	e.seed(t, room{ruleID: "disk-critical", severity: SeverityCritical, status: StatusResolved, assignee: user.ID})
	e.seed(t, room{ruleID: "disk-critical", severity: SeverityCritical, status: StatusAcknowledged})

	for _, tc := range []struct {
		name   string
		filter Filter
	}{
		{"one rule", Filter{RuleID: "disk-critical"}},
		{"one severity", Filter{Severities: []Severity{SeverityCritical}}},
		{"one status", Filter{Statuses: []Status{StatusAcknowledged}}},
		{"one assignee", Filter{AssigneeID: user.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			filter := tc.filter
			filter.OrganizationID = e.org
			page := e.queue(t, filter)
			assert.Contains(t, ids(page), wanted)
			assert.Lenf(t, page.Incidents, 4, "%s must narrow to the rooms that match it", tc.name)
		})
	}

	t.Run("every axis at once", func(t *testing.T) {
		page := e.queue(t, Filter{
			OrganizationID: e.org, RuleID: "disk-critical",
			Severities: []Severity{SeverityCritical},
			Statuses:   []Status{StatusAcknowledged},
			AssigneeID: user.ID,
		})
		assert.Equal(t, []uuid.UUID{wanted}, ids(page))
	})
}

func TestQueueNarrowsToOneMachine(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	others := e.fleet(t, 1)

	// One estate-wide room the machine contributed to, and one it did not.
	e.recordUnder(t, e.variant(nil), perCustomer, Stored)
	joined := e.roomFor(t, perCustomer, "disk-critical", e.org).ID
	elsewhere := e.seed(t, room{ruleID: "cpu-saturated"})

	page := e.queue(t, Filter{OrganizationID: e.org, DeviceID: e.device})
	assert.Equal(t, []uuid.UUID{joined}, ids(page), "the strip shows the rooms this machine is in")

	page = e.queue(t, Filter{OrganizationID: e.org, DeviceID: others[0]})
	assert.Empty(t, ids(page), "a machine that raised nothing is in no room")
	assert.NotContains(t, ids(page), elsewhere)
}

func TestQueueStopsAtTheTenantWall(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	next := e.neighbour(t, "Northwind")
	theirs := e.openRoomIn(t, next, StatusNew, e.now)

	page := e.queue(t, Filter{})
	assert.NotContains(t, ids(page), theirs)

	_, err := e.alerts.Investigation(e.ctx, theirs, uuid.Nil)
	assert.ErrorIs(t, err, ErrIncidentNotFound, "a crafted id from another tenant resolves to nothing")
}

func TestQueueNeverReturnsAnotherCustomersRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	fabrikam := testutil.SeedOrganization(t, e.ctx, e.store, "Fabrikam")

	contoso := e.seed(t, room{org: e.org})
	other := e.seed(t, room{org: fabrikam})

	assert.Equal(t, []uuid.UUID{contoso}, ids(e.queue(t, Filter{OrganizationID: e.org})))
	assert.Equal(t, []uuid.UUID{other}, ids(e.queue(t, Filter{OrganizationID: fabrikam})))

	_, err := e.alerts.Investigation(e.ctx, contoso, fabrikam)
	assert.ErrorIs(t, err, ErrIncidentNotFound,
		"a room read while looking at another customer is not that customer's room")

	// Unnarrowed, one tenant's technician sees both customers; the customer is a filter.
	assert.Len(t, e.queue(t, Filter{}).Incidents, 2)
}

func TestQueuePagesDoNotSkipOrRepeatWhileTheQueueMoves(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	seeded := make([]uuid.UUID, 0, 6)
	for i := range 6 {
		seeded = append(seeded, e.seed(t, room{lastSeen: e.now.Add(-time.Duration(i) * time.Minute)}))
	}

	first := e.queue(t, Filter{OrganizationID: e.org, Limit: 3})
	require.Len(t, first.Incidents, 3)
	require.False(t, first.Next.IsZero(), "a full page offers the cursor its successor starts at")

	// The queue moves: the oldest room fires again and jumps to the head.
	e.exec(t, `UPDATE incidents SET last_seen = $2 WHERE id = $1`,
		seeded[5], e.now.Add(time.Minute))

	second := e.queue(t, Filter{OrganizationID: e.org, Limit: 3, After: first.Next})

	seen := append(ids(first), ids(second)...)
	assert.NotContains(t, ids(second), ids(first)[2], "a page must not repeat the row before it")
	for _, id := range seeded[:5] {
		assert.Containsf(t, seen, id, "room %s was skipped by paging", id)
	}
}

func TestQueuePageIsBounded(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	for i := range maxQueuePage + 5 {
		e.seed(t, room{lastSeen: e.now.Add(-time.Duration(i) * time.Second)})
	}

	assert.Len(t, e.queue(t, Filter{Limit: 0}).Incidents, defaultQueuePage,
		"a caller that states no page size gets the default")
	assert.Len(t, e.queue(t, Filter{Limit: 10_000}).Incidents, maxQueuePage,
		"a caller asking for the table gets a page")
	assert.Len(t, e.queue(t, Filter{Limit: -1}).Incidents, defaultQueuePage,
		"a nonsense page size is not a page of nothing")
}

func TestEveryQueueStatementNamesItsTenant(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"queueForCustomerSQL": queueForCustomerSQL,
		"queueForTenantSQL":   queueForTenantSQL,
		"roomSQL":             roomSQL,
		"roomAlertsSQL":       roomAlertsSQL,
		"roomEventsSQL":       roomEventsSQL,
		"roomPeopleSQL":       roomPeopleSQL,
		"assignRoomSQL":       assignRoomSQL,
		"alertEvidenceSQL":    alertEvidenceSQL,
	} {
		assert.Containsf(t, query, tenantPredicate,
			"%s must name the tenant as well as passing the policy", name)
	}
}

func TestTheRoomNamesWhatItIsAboutTheWayTheQueueDoes(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"queueForCustomerSQL": queueForCustomerSQL,
		"queueForTenantSQL":   queueForTenantSQL,
		"roomSQL":             roomSQL,
	} {
		assert.Containsf(t, query, scopeNameColumn, "%s names its scope its own way", name)
	}
}

func TestQueueAtTenThousandRoomsIsAnIndexedRead(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)
	e.seedOpenRooms(t, 10_000, user.ID)
	e.recordUnder(t, e.variant(nil), perCustomer, Stored)
	e.analyze(t)

	for _, tc := range []struct {
		name   string
		filter Filter
		// ordered marks the reads whose page must come out of an index already in order.
		ordered bool
	}{
		{"the default queue", Filter{OrganizationID: e.org}, true},
		{"the second page", Filter{OrganizationID: e.org, After: Cursor{LastSeen: e.now, ID: uuid.New()}}, true},
		{"every customer at once", Filter{}, true},
		{"one status", Filter{OrganizationID: e.org, Statuses: []Status{StatusNew}}, true},
		{"several statuses", Filter{OrganizationID: e.org, Statuses: OpenStatuses()}, true},
		{"one severity", Filter{OrganizationID: e.org, Severities: []Severity{SeverityCritical}}, true},
		{"one assignee", Filter{OrganizationID: e.org, AssigneeID: user.ID}, true},
		// A rule or a machine selects a set small enough to sort.
		{"one rule", Filter{OrganizationID: e.org, RuleID: "disk-critical"}, false},
		{"one machine", Filter{OrganizationID: e.org, DeviceID: e.device}, false},
		{"every axis at once", Filter{
			OrganizationID: e.org, RuleID: "disk-critical", AssigneeID: user.ID,
			Statuses: OpenStatuses(), Severities: []Severity{SeverityCritical}, DeviceID: e.device,
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := e.planFor(t, tc.filter)
			// The incidents table grows with the estate, so a page never scans it whole.
			assert.NotContainsf(t, plan, "Seq Scan on incidents",
				"%s must not read the incident table to answer a page:\n%s", tc.name, plan)
			assert.Containsf(t, plan, "on incidents",
				"%s must reach the incidents through an index:\n%s", tc.name, plan)
			if tc.ordered {
				assert.Containsf(t, plan, "Index Scan using idx_incidents_",
					"%s must be answered from one of the queue's own indexes:\n%s", tc.name, plan)
				assert.NotContainsf(t, plan, "Sort",
					"%s must come out of the index in order, not be sorted into it:\n%s", tc.name, plan)
			}
		})
	}
}

// seedOpenRooms writes n rooms spread across the statuses, severities and rules a queue narrows by.
func (e estate) seedOpenRooms(t *testing.T, n int, assignee uuid.UUID) {
	t.Helper()
	e.exec(t,
		`INSERT INTO incidents (id, tenant_id, organization_id, rule_id, scope, scope_key,
		                        severity, status, assignee_id, opened_at, first_seen, last_seen,
		                        occurrences, device_count)
		 SELECT gen_random_uuid(), $1, $2, (ARRAY['disk-critical','cpu-saturated','memory-exhausted'])[1 + g % 3],
		        'device', gen_random_uuid(),
		        (ARRAY['info','warning','critical'])[1 + g % 3],
		        (ARRAY['new','acknowledged','investigating'])[1 + g % 3],
		        CASE WHEN g % 4 = 0 THEN $3::uuid END,
		        $4::timestamptz - make_interval(secs => g),
		        $4::timestamptz - make_interval(secs => g),
		        $4::timestamptz - make_interval(secs => g),
		        1, 1
		   FROM generate_series(1, $5) AS g`,
		e.tenant, e.org, assignee, e.now, n)
}

// analyze hands the planner current statistics so it plans against the seeded table.
func (e estate) analyze(t *testing.T) {
	t.Helper()
	e.exec(t, `ANALYZE incidents`)
	e.exec(t, `ANALYZE alerts`)
}

// planFor asks the database how it would answer a page, through the production read scope.
func (e estate) planFor(t *testing.T, f Filter) string {
	t.Helper()
	query, args := queueQuery(f.normalized())

	var plan string
	require.NoError(t, dbtx.Scoped(e.ctx, e.store.DB(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(e.ctx, "EXPLAIN "+query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			plan += line + "\n"
		}
		return rows.Err()
	}))
	return plan
}

func TestQueueSurvivesAStoreThatIsGone(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	closed, err := sql.Open("pgx", "postgres://nobody@127.0.0.1:1/none")
	require.NoError(t, err)
	require.NoError(t, closed.Close())

	store := &Store{db: closed, now: e.alerts.now}
	ctx := dbtx.WithDefaultTenant(context.Background(), false)

	page, err := store.Queue(ctx, Filter{})
	require.Error(t, err)
	assert.Empty(t, page.Incidents, "a queue that could not be read is not an empty queue")
	_, err = store.Investigation(ctx, uuid.New(), uuid.Nil)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrIncidentNotFound,
		"a store that is unreachable must not read as a room that does not exist")
}
