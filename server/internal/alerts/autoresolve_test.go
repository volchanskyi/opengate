package alerts

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When the system is allowed to end a room on its own.
//
// A room may only be closed once nothing could still fold into it, which is why
// the hold is the rule's own grouping window rather than a number of its own:
// any other figure makes auto-resolve and grouping disagree, and WS-4471's
// thirty freezes fragment into thirty rooms. And silence is not recovery — a
// machine told to go quiet has gone quiet for the reason the host work is
// happening.

// aRoomHeldBy opens a room from a single alert and returns it alongside the
// hold the sweep must respect. The two numbers come from one place on purpose —
// a room stays open for exactly as long as a new alert could still fold into it
// — and every case below turns on that being true.
func (e estate) aRoomHeldBy(t *testing.T, g Grouping) (Incident, map[string]time.Duration) {
	t.Helper()
	e.recordUnder(t, e.variant(nil), g, Stored)
	return e.roomFor(t, g, "disk-critical", e.scopeKeyFor(g)), map[string]time.Duration{"disk-critical": g.Window}
}

// scopeKeyFor is what a room about this fixture's alert is keyed on.
func (e estate) scopeKeyFor(g Grouping) uuid.UUID {
	if g.Scope == ScopeOrganization {
		return e.org
	}
	return e.device
}

// TestAutoResolveWaitsOutTheWholeReopenWindow drives C6. The hold is the rule's
// own grouping window, so a room stays open for exactly as long as a new alert
// could still fold into it. A shorter hold closes rooms that were about to
// receive their next occurrence, which is how a recurrence becomes a queue of
// one-offs.
func TestAutoResolveWaitsOutTheWholeReopenWindow(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeDevice, Window: 24 * time.Hour})

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour).Add(-time.Second), windows, 0)
	status, _, _ := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusNew), status, "a second short of the window is still inside it")

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour), windows, 1)
	status, cause, resolvedAt := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusResolved), status)
	assert.False(t, cause.Valid,
		"a cause code is a person's answer, and the system must not put words in their mouth")
	require.True(t, resolvedAt.Valid)
	assert.Equal(t, room.LastSeen.UTC().Add(24*time.Hour), resolvedAt.Time.UTC(),
		"it closed when it became closeable, not when the sweep happened to run")

	history := e.history(t, room.ID)
	require.Len(t, history, 1)
	assert.Equal(t, "resolution", history[0].kind)
	assert.Empty(t, history[0].actor, "nobody did this, so nobody is named")

	// A second pass over the same rooms closes nothing twice.
	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 0)
}

// TestOneAlertInsideTheWindowResetsTheClock is the other half of C6. The hold is
// measured from the last thing that happened, not from when the room opened, or
// a slow burn that keeps re-firing would close underneath the technician working
// it.
func TestOneAlertInsideTheWindowResetsTheClock(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	grouping := Grouping{Scope: ScopeDevice, Window: 24 * time.Hour}
	room, windows := e.aRoomHeldBy(t, grouping)

	halfway := room.LastSeen.Add(12 * time.Hour)
	e.recordUnder(t, e.variant(at(halfway)), grouping, Stored)

	e.sweepAt(t, room.LastSeen.Add(24*time.Hour), windows, 0)
	status, _, _ := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusNew), status, "the hold runs from the last occurrence")

	e.sweepAt(t, halfway.Add(24*time.Hour), windows, 1)
}

// TestAMachineToldToGoQuietKeepsItsRoom drives E5. Maintenance stops the agent
// sampling, so the silence that follows is the silence the operator asked for.
// Reading it as recovery closes the very incident the host work is happening
// because of, and the technician comes back to a queue that says everything is
// fine.
func TestAMachineToldToGoQuietKeepsItsRoom(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeDevice, Window: 24 * time.Hour})
	e.exec(t, `UPDATE devices SET maintenance_on = TRUE WHERE id = $1`, e.device)

	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 0)
	status, _, _ := e.outcome(t, room.ID)
	assert.Equal(t, string(StatusNew), status,
		"an incident open before maintenance does not auto-resolve during it")

	// Coming back out, the ordinary hold applies again.
	e.exec(t, `UPDATE devices SET maintenance_on = FALSE WHERE id = $1`, e.device)
	e.sweepAt(t, room.LastSeen.Add(72*time.Hour), windows, 1)
}

// TestMaintenanceShieldsOnlyTheRoomThatIsAboutThatMachine keeps E5 from becoming
// a hole. One machine going quiet says nothing about an estate-wide room the
// rest of the customer's fleet is still reporting into, so a site or customer
// room holds no shield — otherwise a single machine parked in maintenance would
// pin an estate's rooms open indefinitely.
func TestMaintenanceShieldsOnlyTheRoomThatIsAboutThatMachine(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room, windows := e.aRoomHeldBy(t, Grouping{Scope: ScopeOrganization, Window: 30 * time.Minute})
	e.exec(t, `UPDATE devices SET maintenance_on = TRUE WHERE id = $1`, e.device)

	e.sweepAt(t, room.LastSeen.Add(time.Hour), windows, 1)
}

// TestASweepLeavesARoomItHasNoWindowFor is what happens to a room raised by a
// rule this build no longer ships. There is nothing to measure the hold against,
// and guessing one would close a customer's open work on a number nobody chose,
// so it is left for a person.
func TestASweepLeavesARoomItHasNoWindowFor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room := e.openRoomAt(t, StatusNew, e.now.Add(-365*24*time.Hour))

	e.sweepAt(t, e.now, map[string]time.Duration{"some-other-rule": time.Minute}, 0)
	status, _, _ := e.outcome(t, room)
	assert.Equal(t, string(StatusNew), status)
}

// TestTheStormRoomClosesItselfOnceTheHourIsQuiet gives the one room no catalogue
// rule can supply a window for its own hold. A storm is about a rolling hour, so
// an hour with nothing refused is a storm that is over.
func TestTheStormRoomClosesItselfOnceTheHourIsQuiet(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	e.seedHourOfAlerts(t, OrganizationHourlyCeiling, e.now.Add(-10*time.Minute))
	e.record(t, e.alert(), CeilingSuppressed)
	storm := e.roomFor(t, Grouping{Scope: ScopeOrganization}, StormRuleID, e.org)

	e.sweepAt(t, e.now.Add(59*time.Minute), nil, 0)
	e.sweepAt(t, e.now.Add(time.Hour), nil, 1)

	status, _, _ := e.outcome(t, storm.ID)
	assert.Equal(t, string(StatusResolved), status)
}

// TestTheSweepReachesEveryTenant pins the janitor's one deliberate difference
// from every other statement in the store: it is asked about the whole database
// rather than one customer, because a stale room in a tenant nobody is currently
// serving requests for still sits in that tenant's triage queue.
func TestTheSweepReachesEveryTenant(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	windows := map[string]time.Duration{"disk-critical": time.Hour}

	mine := e.openRoomAt(t, StatusNew, e.now.Add(-2*time.Hour))
	other := e.neighbour(t, "Fabrikam")
	theirs := e.openRoomIn(t, other, StatusNew, e.now.Add(-2*time.Hour))

	e.sweepAt(t, e.now, windows, 2)

	status, _, _ := e.outcome(t, mine)
	assert.Equal(t, string(StatusResolved), status)
	statusOther, _, _ := e.outcomeIn(t, other, theirs)
	assert.Equal(t, string(StatusResolved), statusOther,
		"a tenant with nobody logged in still has its queue kept honest")
}

// TestAnUnreachableStoreIsReportedByEveryDoor extends E19's rule from ingest to
// the rest of the engine. A move that failed and reported success would leave
// two technicians with different beliefs about who owns a room, and a sweep that
// swallowed a failure would report a queue as tidy while it grew.
func TestAnUnreachableStoreIsReportedByEveryDoor(t *testing.T) {
	t.Parallel()
	e := newEstate(t)
	room := e.openRoomAt(t, StatusNew, e.now)
	closed := e.openRoomAt(t, StatusResolved, e.now)

	require.NoError(t, e.store.Close())

	assert.Error(t, e.alerts.Transition(e.ctx, room, Change{To: StatusAcknowledged}),
		"a move nobody made must not read as one that succeeded")
	assert.Error(t, e.alerts.Reopen(e.ctx, closed, uuid.New()))
	_, err := e.alerts.ResolveStale(context.Background(), map[string]time.Duration{"disk-critical": time.Hour})
	assert.Error(t, err, "a sweep that could not run must not report a tidy queue")
}

// TestTheStormHoldIsTheStoresOwn keeps the one room no rule describes out of the
// caller's hands. Every other hold is the rule's grouping window, but the storm
// room is not a rule — it is a rolling hour's budget being spent — so a caller
// naming it cannot lengthen or shorten what that means.
func TestTheStormHoldIsTheStoresOwn(t *testing.T) {
	t.Parallel()

	rules, seconds := holds(map[string]time.Duration{
		StormRuleID:     time.Minute,
		"disk-critical": 15 * time.Minute,
		"never-set":     0,
	})

	require.Len(t, rules, 2)
	byRule := map[string]float64{}
	for i, id := range rules {
		byRule[id] = seconds[i]
	}
	assert.Equal(t, StormHold.Seconds(), byRule[StormRuleID], "the storm's hour is not the caller's to set")
	assert.InDelta(t, (15 * time.Minute).Seconds(), byRule["disk-critical"], 0)
	assert.NotContains(t, byRule, "never-set", "a rule declaring no window closes nothing")
}
