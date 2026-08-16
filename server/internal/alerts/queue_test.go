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

// What the triage queue has to guarantee, driven against a real database.
//
// A queue is read while it is being written to, which is the whole difficulty:
// an alert lands, a room's last activity moves, and the row a technician was
// about to page past moves with it. So the page is a keyset rather than an
// offset — an offset over a moving queue drops rows silently, and the row it
// drops is an incident nobody sees.
//
// The other half is who sees what. Isolation is the tenant's and the database
// enforces it; narrowing to one customer is a filter a query has to get right,
// and getting it wrong shows one customer's estate to a technician looking at
// another's without anything being refused.

// seedRoom writes one room with everything a queue filter can narrow on, so a
// case states only the axis it is about.
type room struct {
	org      uuid.UUID
	ruleID   string
	severity Severity
	status   Status
	assignee uuid.UUID
	lastSeen time.Time
}

// seed writes the room and returns its id.
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
	// Filed at the machine rung with a key of its own, so a case can seed as many
	// rooms as it likes without colliding on the one-open-room-per-key index.
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

// queue reads a page and fails the case if the read itself does.
func (e estate) queue(t *testing.T, f Filter) Page {
	t.Helper()
	page, err := e.alerts.Queue(e.ctx, f)
	require.NoError(t, err)
	return page
}

// ids renders a page as the rooms it holds, which is what every case here
// asserts on.
func ids(page Page) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(page.Incidents))
	for _, incident := range page.Incidents {
		out = append(out, incident.ID)
	}
	return out
}

// TestQueueAnswersNewestActivityFirst pins the order the triage queue is read
// in. It is last activity rather than when a room opened: a week-old room that
// fired again this morning is today's work, and one that opened this morning and
// went quiet is not.
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

// TestQueueCarriesWhatTheQueueIsReadFor asserts a listed room says everything a
// technician triages on without a second read. A queue whose rows have to be
// enriched one by one is a queue that costs a round trip per row to render.
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

// TestQueueNarrowsOnEveryAxisTheUICanOffer walks each filter on its own. They
// are separate cases rather than one combined read because a filter that
// silently narrows nothing passes any test where another filter did the work.
func TestQueueNarrowsOnEveryAxisTheUICanOffer(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	user := testutil.SeedUser(t, e.ctx, e.store)

	wanted := e.seed(t, room{
		ruleID: "disk-critical", severity: SeverityCritical,
		status: StatusAcknowledged, assignee: user.ID, lastSeen: e.now,
	})
	// One neighbour per axis, each differing from the wanted room in exactly the
	// thing its case filters on.
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

// TestQueueNarrowsToOneMachine is the device page's strip, asked of the queue
// rather than of a second implementation. A room is not keyed on a machine — a
// customer-wide event is one room across forty of them — so the question is
// which rooms hold an alert this machine raised.
func TestQueueNarrowsToOneMachine(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	others := e.fleet(t, 1)

	// One estate-wide room the machine under test contributed to, and one it did
	// not: the strip must show the first and not the second.
	e.recordUnder(t, e.variant(nil), perCustomer, Stored)
	joined := e.roomFor(t, perCustomer, "disk-critical", e.org).ID
	elsewhere := e.seed(t, room{ruleID: "cpu-saturated"})

	page := e.queue(t, Filter{OrganizationID: e.org, DeviceID: e.device})
	assert.Equal(t, []uuid.UUID{joined}, ids(page), "the strip shows the rooms this machine is in")

	page = e.queue(t, Filter{OrganizationID: e.org, DeviceID: others[0]})
	assert.Empty(t, ids(page), "a machine that raised nothing is in no room")
	assert.NotContains(t, ids(page), elsewhere)
}

// TestQueueStopsAtTheTenantWall proves the isolation half. A room in another
// tenant answers exactly as one that does not exist, including when its id is
// named outright — a caller who guesses an id is the case this is for.
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

// TestQueueNeverReturnsAnotherCustomersRoom is the other kind of leak, and the
// quieter one: both customers are inside one tenant, so the wall is not
// breached and nothing is refused — a wrong query simply shows Contoso's
// estate to somebody looking at Fabrikam's.
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

	// Unnarrowed, one tenant's technician sees both customers — the customer is a
	// filter, not a wall.
	assert.Len(t, e.queue(t, Filter{}).Incidents, 2)
}

// TestQueuePagesDoNotSkipOrRepeatWhileTheQueueMoves is why the page is a keyset.
//
// Between two pages an alert lands and a room's last activity moves to the head
// of the queue. Under an offset every row behind it shifts by one, so the first
// row of the second page is one nobody ever saw — silently, since a queue does
// not report the rows it did not return.
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

// TestQueuePageIsBounded keeps one read from becoming the whole table. A caller
// asking for everything is answered with a page and a cursor, which is the same
// answer a caller asking for a sensible number gets.
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

// TestEveryQueueStatementNamesItsTenant extends the wall the alert and incident
// statements already stand behind, to the reads a person makes.
func TestEveryQueueStatementNamesItsTenant(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]string{
		"queueForCustomerSQL": queueForCustomerSQL,
		"queueForTenantSQL":   queueForTenantSQL,
		"roomSQL":             roomSQL,
		"roomAlertsSQL":       roomAlertsSQL,
		"roomEventsSQL":       roomEventsSQL,
		"assignRoomSQL":       assignRoomSQL,
		"alertEvidenceSQL":    alertEvidenceSQL,
	} {
		assert.Containsf(t, query, tenantPredicate,
			"%s must name the tenant as well as passing the policy", name)
	}
}

// TestQueueAtTenThousandRoomsIsAnIndexedRead is Q10, asserted as a plan rather
// than as a stopwatch.
//
// A wall-clock assertion inside a unit suite is flaky by construction — it
// measures the machine it runs on — while the plan is what actually keeps the
// budget: a page of fifty read in last-activity order from a customer-leading
// index costs the same at ten thousand rooms as at ten. A sequential scan
// passes at ten thousand on a fast laptop and misses the budget on the estate
// this is sized for, which is exactly the failure a timing test cannot catch.
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
		// ordered marks the reads whose page must come out of an index already
		// in order. These are the ones the budget rests on: an unnarrowed queue
		// answered by sorting every room a customer has costs the whole table to
		// return fifty rows, and that cost grows with the estate.
		ordered bool
	}{
		{"the default queue", Filter{OrganizationID: e.org}, true},
		{"the second page", Filter{OrganizationID: e.org, After: Cursor{LastSeen: e.now, ID: uuid.New()}}, true},
		{"every customer at once", Filter{}, true},
		{"one status", Filter{OrganizationID: e.org, Statuses: []Status{StatusNew}}, true},
		{"several statuses", Filter{OrganizationID: e.org, Statuses: OpenStatuses()}, true},
		{"one severity", Filter{OrganizationID: e.org, Severities: []Severity{SeverityCritical}}, true},
		{"one assignee", Filter{OrganizationID: e.org, AssigneeID: user.ID}, true},
		// The narrow ones are allowed to sort: a rule or a machine selects a
		// small enough set that ordering it costs less than walking the queue
		// index until fifty of them turn up.
		{"one rule", Filter{OrganizationID: e.org, RuleID: "disk-critical"}, false},
		{"one machine", Filter{OrganizationID: e.org, DeviceID: e.device}, false},
		{"every axis at once", Filter{
			OrganizationID: e.org, RuleID: "disk-critical", AssigneeID: user.ID,
			Statuses: OpenStatuses(), Severities: []Severity{SeverityCritical}, DeviceID: e.device,
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := e.planFor(t, tc.filter)
			// The incidents table is the one that grows with the estate, so it is
			// the one that may never be read whole. A page of fifty that scans it
			// passes at ten thousand rooms on a fast laptop and misses the budget
			// on the estate this is sized for.
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

// seedOpenRooms writes n rooms spread across the statuses, severities and rules
// a queue is narrowed by, so the planner sees a distribution rather than one
// value repeated ten thousand times.
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

// analyze hands the planner current statistics. Without them it plans against
// an empty table and picks a scan for a reason the assertion is not about.
func (e estate) analyze(t *testing.T) {
	t.Helper()
	e.exec(t, `ANALYZE incidents`)
	e.exec(t, `ANALYZE alerts`)
}

// planFor asks the database how it would answer a page, through the same scope
// production reads pass.
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

// TestQueueSurvivesAStoreThatIsGone keeps a broken deployment loud rather than
// answering an empty triage queue, which is the one wrong answer nobody
// questions.
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
