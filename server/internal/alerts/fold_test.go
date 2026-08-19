package alerts

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// What the fold has to hold true besides landing an alert in the right room:
// that two connections cannot split one event, that a rule upgrade cannot fork
// a live room, that a retroactive scan is one finding rather than a queue of
// them, and that an alert and its room are one write or neither.

// TestConcurrentFoldOpensExactlyOneRoom proves the fold is race-safe through the
// partial unique index rather than through a mutex, which would hold only for as
// long as one server process is the only writer. Contoso's forty machines report
// on forty connections at once; two of them opening two rooms for one event
// splits an estate-wide incident nobody can then reconcile.
func TestConcurrentFoldOpensExactlyOneRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	const writers = 8
	fleet := e.fleet(t, writers)
	grouping := Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute}

	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make([]Outcome, 0, writers)
		failed  []error
	)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			when := e.now.Add(time.Duration(i) * time.Minute)
			alert := e.variant(func(a *Alert) {
				a.DeviceID = fleet[i]
				at(when)(a)
			})
			<-start
			outcome, err := e.alerts.Record(e.ctx, alert, grouping)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			results = append(results, outcome)
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, failed, "a concurrent fold must not fail, it must converge")
	assert.Len(t, results, writers)
	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeOrganization),
		"the database's own index is what makes one key one room")
	room := e.roomFor(t, grouping, "disk-critical", e.org)
	assert.Equal(t, writers, room.Occurrences, "every concurrent alert is counted exactly once")
	assert.Equal(t, writers, room.DeviceCount)
}

// TestRuleUpgradeDoesNotForkALiveRoom drives E16. A curated rule is retuned while
// somebody is working an incident it raised. Keying the room on the version would
// silently open a second one, and the technician's notes would stay in the room
// nothing arrives in any more.
func TestRuleUpgradeDoesNotForkALiveRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	e.recordUnder(t, e.variant(func(a *Alert) {
		a.RuleVersion = 4
		shifted(10 * time.Minute)(a)
		a.ObservedAt = a.ObservedAt.Add(10 * time.Minute)
	}), grouping, Stored)

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice),
		"a rule upgrade must not fork the room somebody is working in")
	assert.Equal(t, 2, e.roomFor(t, grouping, "disk-critical", e.device).Occurrences)
}

// TestTwoRulesOnOneConditionStayTwoRooms drives E15. A machine out of memory
// trips the memory rule and the CPU rule at once. They are two findings with two
// remedies, and merging them would hide whichever the technician did not read.
func TestTwoRulesOnOneConditionStayTwoRooms(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	e.recordUnder(t, e.variant(func(a *Alert) { a.RuleID = "memory-pressure" }), grouping, Stored)

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice))
	assert.Equal(t, 1, e.rooms(t, "memory-pressure", ScopeDevice))
	assert.NotEqual(t,
		e.roomFor(t, grouping, "disk-critical", e.device).ID,
		e.roomFor(t, grouping, "memory-pressure", e.device).ID)
}

// TestBackfilledFindingsFoldByEventTime drives E8 and the second half of B9. A
// rule arriving on WS-4471 is re-run over the month of history the machine
// already holds, and every finding arrives in the same second. Judging the fold
// against the clock would read them as thirty things happening now and open
// thirty rooms; judging it against when each one happened is what makes a whole
// retroactive scan one room.
//
// The findings are replayed newest-first, because a scan walking history
// backwards is the ordering that breaks a fold written only to extend forwards.
func TestBackfilledFindingsFoldByEventTime(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 7 * 24 * time.Hour}

	for day := range 30 {
		when := e.now.Add(-time.Duration(day+1) * 24 * time.Hour)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.RuleID = "workstation-freeze"
			a.Backfilled = true
			at(when)(a)
		}), grouping, Stored)
	}

	assert.Equal(t, 1, e.rooms(t, "workstation-freeze", ScopeDevice),
		"a retroactive scan is one finding, not a queue of thirty")
	room := e.roomFor(t, grouping, "workstation-freeze", e.device)
	assert.Equal(t, 30, room.Occurrences)
	assert.Equal(t, e.now.Add(-30*24*time.Hour), room.FirstSeen.UTC(),
		"a week-old freeze stays a week old rather than sorting as today's")
}

// TestAFindingOlderThanItsRoomStaysOutOfIt is the other side of event-time
// folding. A live room is a week old and a retroactive scan turns up something
// from three months before it started. That is not part of the story the room
// tells, and the room is not stale — so the finding is kept and filed under
// nothing rather than closing a live room or being back-dated into it.
func TestAFindingOlderThanItsRoomStaysOutOfIt(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 24 * time.Hour}

	e.recordUnder(t, e.variant(nil), grouping, Stored)
	room := e.roomFor(t, grouping, "disk-critical", e.device)

	ancient := e.now.Add(-90 * 24 * time.Hour)
	old := e.variant(func(a *Alert) {
		a.Backfilled = true
		at(ancient)(a)
	})
	e.recordUnder(t, old, grouping, Stored)

	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice),
		"a finding that predates the live room must not open a second open room")
	assert.Equal(t, 1, e.roomFor(t, grouping, "disk-critical", e.device).Occurrences,
		"nor be counted into a room whose story it is not part of")
	assert.Empty(t, e.roomOf(t, old.ID), "the finding is kept; it simply belongs to no room")
	status, _, _ := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusNew), status, "and the live room is left alone")
}

// TestEveryIncidentStatementNamesItsTenantExceptTheJanitor extends the wall the
// alert statements already stand behind. The sweep is the one deliberate
// exception: it is asked about every tenant at once, so there is no single
// tenant for its predicate to confine it to, and writing one would be a claim it
// does not make. Pinning the exception here is what keeps the list from quietly
// growing a second member.
func TestEveryIncidentStatementNamesItsTenantExceptTheJanitor(t *testing.T) {
	t.Parallel()
	scoped := map[string]string{
		"lockOpenRoomSQL":              lockOpenRoomSQL,
		"closeLapsedRoomSQL":           closeLapsedRoomSQL,
		"attachAlertSQL":               attachAlertSQL,
		"attachPendingObservationsSQL": attachPendingObservationsSQL,
		"countPendingObserversSQL":     countPendingObserversSQL,
		"restateRoomFromItsAlertsSQL":  restateRoomFromItsAlertsSQL,
		"deviceSiteSQL":                deviceSiteSQL,
		"readRoomStatusSQL":            readRoomStatusSQL,
		"applyTransitionSQL":           applyTransitionSQL,
		"appendRoomEventSQL":           appendRoomEventSQL,
	}
	for name, query := range scoped {
		assert.Containsf(t, query, tenantPredicate,
			"%s must name the tenant as well as passing the policy", name)
	}

	// A statement that creates a room names the tenant as the value it writes,
	// which the policy checks on the way in; there is no existing row for a
	// predicate to match against.
	assert.Contains(t, openOrJoinRoomSQL, "tenant_id",
		"a room still belongs to exactly one tenant")

	assert.NotContains(t, resolveStaleRoomsSQL, tenantPredicate,
		"the janitor sweeps every tenant, so naming one would be a claim it does not make")
	assert.Contains(t, resolveStaleRoomsSQL, "maintenance_on",
		"a machine that was told to go quiet must not have its room closed by the quiet")
}

// TestUnknownGroupingIsRefusedRatherThanGuessed keeps a wiring mistake loud. A
// zero window would fold every firing of a rule into one room that never
// resolves, and a scope outside the closed set cannot be stored at all — both
// are a caller bug, and inventing a default for either would bury it.
func TestUnknownGroupingIsRefusedRatherThanGuessed(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	for _, tc := range []struct {
		name     string
		grouping Grouping
	}{
		{"no window at all", Grouping{Scope: ScopeDevice}},
		{"a negative window", Grouping{Scope: ScopeDevice, Window: -time.Hour}},
		{"a scope nothing can store", Grouping{Scope: Scope("tenant"), Window: time.Hour}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.alerts.Record(e.ctx, e.variant(nil), tc.grouping)
			assert.ErrorIs(t, err, ErrGroupingUnusable)
		})
	}
	assert.Zero(t, e.count(t, qCustomerAlerts, e.org), "a refused grouping stores nothing")
}

// TestFoldFailureLeavesNoAlertBehind keeps the alert and its room one write. An
// alert stored outside the room it belongs to is invisible to the only surface a
// technician looks at, which is worse than the alert never arriving: nothing says
// it is missing.
func TestFoldFailureLeavesNoAlertBehind(t *testing.T) {
	t.Parallel()
	e := newEstate(t)

	// A grouping key naming a machine that does not exist cannot resolve a site,
	// which is the fold failing after the alert row has already been written.
	_, err := e.alerts.Record(e.ctx, e.variant(func(a *Alert) { a.DeviceID = uuid.New() }),
		Grouping{Scope: ScopeSite, Window: time.Hour})
	require.Error(t, err)
	assert.Zero(t, e.count(t, qCustomerAlerts, e.org))
	assert.Zero(t, e.rooms(t, "disk-critical", ScopeSite))
}

// TestStormRoomIsNotFoldedIntoByOrdinaryAlerts keeps the two kinds of room
// apart. A storm room counts what was refused and holds no alerts at all; an
// ordinary alert arriving under the same customer must not be counted into it.
func TestStormRoomIsNotFoldedIntoByOrdinaryAlerts(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, DefaultOrganizationHourlyCeiling, e.now.Add(-10*time.Minute))

	e.record(t, e.alert(), CeilingSuppressed)
	storm := e.roomFor(t, Grouping{Scope: ScopeOrganization}, StormRuleID, e.org)
	assert.Equal(t, 1, storm.Occurrences)
	assert.Zero(t, storm.DeviceCount)

	// The budget frees up and an ordinary alert lands in its own room.
	e.alerts.now = func() time.Time { return e.now.Add(2 * time.Hour) }
	e.recordUnder(t, e.variant(shifted(time.Hour)), Grouping{Scope: ScopeDevice, Window: time.Hour}, Stored)

	assert.Equal(t, 1, e.roomFor(t, Grouping{Scope: ScopeOrganization}, StormRuleID, e.org).Occurrences,
		"a stored alert is not a suppressed one and must not be counted as one")
	assert.Equal(t, 1, e.rooms(t, "disk-critical", ScopeDevice))
}

// TestTheRoomIsRestatedFromTheAlertsItHolds pins the one rule that makes the
// numbers on a room survive both of the things that break a counter: a
// concurrent fold, where two increments read the same starting value, and an
// erasure, where a machine's rows leave and no foreign key subtracts them. The
// span is event time throughout, so the room says when the estate's problem
// happened rather than when the rows arrived.
func TestTheRoomIsRestatedFromTheAlertsItHolds(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeOrganization, Window: time.Hour}
	fleet := e.fleet(t, 2)

	for i, id := range []uuid.UUID{e.device, fleet[0], fleet[1]} {
		when := e.now.Add(time.Duration(i) * 10 * time.Minute)
		e.recordUnder(t, e.variant(func(a *Alert) {
			a.DeviceID = id
			at(when)(a)
		}), grouping, Stored)
	}
	room := e.roomFor(t, grouping, "disk-critical", e.org)
	assert.Equal(t, e.now, room.FirstSeen.UTC())
	assert.Equal(t, e.now.Add(20*time.Minute), room.LastSeen.UTC())

	require.NoError(t, e.alerts.EraseDeviceAlerts(e.ctx, e.tenant, e.device))

	status, occurrences, deviceCount := e.room(t, room.ID)
	assert.Equal(t, 2, deviceCount, "the room survives on the machines that are left")
	assert.Equal(t, 2, occurrences)
	assert.Equal(t, string(StatusNew), status, "a room that still holds alerts stays open")
}
